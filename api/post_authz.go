package api

import (
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/macrat/ldapin/config"
)

type PostAuthzRequest struct {
	GetAuthzRequest

	User     string `form:"username" json:"username" xml:"username"`
	Password string `form:"password" json:"password" xml:"password"`
}

func (req *PostAuthzRequest) Bind(c *gin.Context) *ErrorMessage {
	err := c.ShouldBind(req)
	if err != nil {
		return &ErrorMessage{
			Err:         err,
			Reason:      "invalid_request",
			Description: "failed to parse request",
		}
	}
	return nil
}

func (req *PostAuthzRequest) BindAndValidate(c *gin.Context, config *config.LdapinConfig) *ErrorMessage {
	if err := req.Bind(c); err != nil {
		return err
	}
	return req.Validate(config)
}

func (api *LdapinAPI) PostAuthz(c *gin.Context) {
	var req PostAuthzRequest
	if err := (&req).BindAndValidate(c, api.Config); err != nil {
		err.Redirect(c)
		return
	}

	scope := ParseStringSet(req.Scope)
	scope.Add("openid")
	req.Scope = scope.String()

	if req.User == "" || req.Password == "" {
		c.HTML(http.StatusForbidden, "login.tmpl", gin.H{
			"config":           api.Config,
			"request":          req.GetAuthzRequest,
			"initial_username": req.User,
			"error":            "missing_username_or_password",
		})
		return
	}

	conn, err := api.Connector.Connect()
	if err != nil {
		log.Print(err)
		req.makeError(err, "server_error", "failed to connecting LDAP server").Redirect(c)
		return
	}
	defer conn.Close()

	if err := conn.LoginTest(req.User, req.Password); err != nil {
		c.HTML(http.StatusForbidden, "login.tmpl", gin.H{
			"config":           api.Config,
			"request":          req.GetAuthzRequest,
			"initial_username": req.User,
			"error":            "invalid_username_or_password",
		})
		return
	}

	if *api.Config.TTL.SSO > 0 {
		ssoToken, err := api.TokenManager.CreateSSOToken(
			api.Config.Issuer,
			req.User,
			time.Now(),
			time.Duration(*api.Config.TTL.SSO),
		)
		if err == nil {
			secure := api.Config.Issuer.Scheme == "https"
			c.SetCookie(
				"token",
				ssoToken,
				int(api.Config.TTL.SSO.IntSeconds()),
				"/",
				(*url.URL)(api.Config.Issuer).Hostname(),
				secure,
				true,
			)
		}
	}

	resp, errMsg := api.makeAuthzTokens(req.GetAuthzRequest, req.User, time.Now())
	if errMsg != nil {
		errMsg.Redirect(c)
	}

	c.Redirect(http.StatusFound, resp.String())
}
