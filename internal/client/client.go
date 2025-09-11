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
	request     *resty.Request
}

func NewRestyClient(cfg *config.Config) *Client {

	restyClient := resty.New().
		SetTimeout(15*time.Second).
		SetHeader("User-Agent", "app/1 CFNetwork/3826.600.41 Darwin/24.6.0").
		SetHeader("Accept", "image/jpeg")

	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}
	restyClient.SetTransport(transport)

	req := restyClient.R()
	if cfg.Authorization.Token != "" {
		req.SetHeader("Authorization", cfg.Authorization.Token)
	}

	cookieName, cookieValue := parseCookie(cfg.Authorization.Cookie)
	if cookieValue != "" {
		req.SetCookie(&http.Cookie{
			Name:  cookieName,
			Value: cookieValue,
		})
	}

	return &Client{
		restyClient: restyClient,
		request:     req,
	}
}

func (c *Client) GetStream(url string) (*resty.Response, error) {
	return c.request.Get(url)
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
