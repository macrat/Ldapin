package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/macrat/lauth/config"
	"github.com/macrat/lauth/errors"
	"github.com/macrat/lauth/metrics"
	"github.com/rs/zerolog/log"
)

func (api *LauthAPI) userinfo(subject string, scope *StringSet) (map[string]interface{}, *errors.Error) {
	conn, err := api.Connector.Connect()
	if err != nil {
		log.Error().
			Err(err).
			Msg("failed to connecting LDAP server")

		return nil, &errors.Error{
			Err:         err,
			Reason:      errors.ServerError,
			Description: "failed to get user info",
		}
	}
	defer conn.Close()

	attrs, err := conn.GetUserAttributes(subject, api.Config.Scopes.AttributesFor(scope.List()))
	if err != nil {
		return nil, &errors.Error{
			Err:         err,
			Reason:      errors.InvalidToken,
			Description: "user was not found or disabled",
		}
	}

	maps := api.Config.Scopes.ClaimMapFor(scope.List())
	result := config.MappingClaims(attrs, maps)
	result["sub"] = subject

	return result, nil
}

func (api *LauthAPI) sendUserInfo(c *gin.Context, report *metrics.Context, origin, rawToken string) {
	token, err := api.TokenManager.ParseAccessToken(rawToken)
	if err == nil {
		report.Set("username", token.Subject)
		err = token.Validate(api.Config.Issuer)
	}

	clientID := ""
	if len(token.AuthorizedParties) > 0 {
		clientID = token.AuthorizedParties[0]
		report.Set("client_id", clientID)
	}

	if err != nil {
		e := &errors.Error{
			Err:         err,
			Reason:      errors.InvalidToken,
			Description: "token is invalid",
		}
		report.SetError(e)
		errors.SendJSON(c, e)
		return
	}

	if origin != "" {
		client := api.Config.Clients[clientID]
		if client.CORSOrigin.Match(origin) {
			c.Header("Access-Control-Allow-Origin", origin)
		} else {
			e := &errors.Error{
				Reason:      errors.AccessDenied,
				Description: "Origin is not registered as a valid client",
			}
			report.SetError(e)
			c.JSON(http.StatusForbidden, e)
			return
		}
	}

	scope := ParseStringSet(token.Scope)
	info, e := api.userinfo(token.Subject, scope)
	if e != nil {
		report.SetError(e)
		errors.SendJSON(c, e)
		return
	}

	report.Success()
	c.JSON(http.StatusOK, info)
}
