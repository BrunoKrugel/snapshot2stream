package client

import (
	"net/http"
	"strings"
	"time"

	"github.com/BrunoKrugel/snapshot2stream/internal/config"
	"github.com/go-resty/resty/v2"
)

type Client struct {
	restyClient *resty.Client
	authToken   string
	cookieName  string
	cookieValue string
}

func NewRestyClient(cfg *config.Config) *Client {

	restyClient := resty.New().
		SetTimeout(5*time.Second).
		SetHeader("User-Agent", "app/1 CFNetwork/3826.600.41 Darwin/24.6.0").
		SetHeader("Accept", "image/jpeg").
		SetRetryCount(2).
		SetRetryWaitTime(50 * time.Millisecond).
		SetDisableWarn(true)

	transport := &http.Transport{
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	restyClient.SetTransport(transport)

	cookieName, cookieValue := parseCookie(cfg.Authorization.Cookie)

	return &Client{
		restyClient: restyClient,
		authToken:   cfg.Authorization.Token,
		cookieName:  cookieName,
		cookieValue: cookieValue,
	}
}

func (c *Client) GetStream(url string) (*resty.Response, error) {
	req := c.restyClient.R()
	
	if c.authToken != "" {
		req.SetHeader("Authorization", c.authToken)
	}
	
	if c.cookieValue != "" {
		req.SetCookie(&http.Cookie{
			Name:  c.cookieName,
			Value: c.cookieValue,
		})
	}
	
	return req.Get(url)
}

func parseCookie(s string) (name, value string) {
	if s == "" {
		return "", ""
	}
	if strings.Contains(s, "=") {
		parts := strings.SplitN(s, "=", 2)
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "SessaoId", s
}
