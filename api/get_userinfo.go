package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/macrat/lauth/metrics"
)

type GetUserInfoRequest struct {
	Authorization string `form:"-" header:"Authorization"`
}

func (req *GetUserInfoRequest) Bind(c *gin.Context) *ErrorMessage {
	if err := c.ShouldBindHeader(req); err != nil {
		return &ErrorMessage{
			Err:         err,
			Reason:      InvalidToken,
			Description: "access token is required",
		}
	}

	return nil
}

func (req GetUserInfoRequest) GetToken() (string, *ErrorMessage) {
	if !strings.HasPrefix(req.Authorization, "Bearer ") {
		return "", &ErrorMessage{
			Reason:      InvalidToken,
			Description: "access token is required",
		}
	}

	return strings.TrimSpace(req.Authorization[len("Bearer "):]), nil
}

func (api *LauthAPI) GetUserInfo(c *gin.Context) {
	report := metrics.StartUserinfo(c)
	defer report.Close()

	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")

	var req GetUserInfoRequest
	if errMsg := (&req).Bind(c); errMsg != nil {
		c.Header("WWW-Authenticate", "Bearer error=\"invalid_token\",error_description=\"access token is required\"")
		errMsg.Report(report)
		errMsg.JSON(c)
		return
	}

	rawToken, errMsg := req.GetToken()
	if errMsg != nil {
		c.Header("WWW-Authenticate", "Bearer error=\"invalid_token\",error_description=\"access token is required\"")
		errMsg.Report(report)
		errMsg.JSON(c)
		return
	}

	result, errMsg := api.userinfoByToken(rawToken, report)
	if errMsg != nil {
		if errMsg.Reason == InvalidToken {
			c.Header("WWW-Authenticate", "Bearer error=\"invalid_token\",error_description=\"token is invalid\"")
		}
		errMsg.Report(report)
		errMsg.JSON(c)
	} else {
		report.Success()
		c.JSON(http.StatusOK, result)
	}
}
