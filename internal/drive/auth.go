package drive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// LoginResult 扫码登录信息（verification_uri 为二维码图片 URL）。
type LoginResult struct {
	VerificationURI string `json:"verification_uri"`
	UserCode        string `json:"user_code"`
	ExpiresIn       int32  `json:"expires_in"`
	Interval        int32  `json:"interval"`
}

// PollResult 轮询扫码状态。
type PollResult struct {
	Status   string `json:"status"`
	LoggedIn bool   `json:"logged_in"`
}

// StartLogin 发起 115 扫码（网页/App，不用开放平台）。
func (c *Client) StartLogin(ctx context.Context) (*LoginResult, error) {
	b, err := c.do(ctx, http.MethodGet, apiQRToken, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		State int `json:"state"`
		Data  struct {
			UID    string `json:"uid"`
			Time   int64  `json:"time"`
			Sign   string `json:"sign"`
			QRCode string `json:"qrcode"`
		} `json:"data"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("解析二维码: %w", err)
	}
	if resp.State != 1 || resp.Data.UID == "" {
		msg := resp.Error
		if msg == "" {
			msg = resp.Message
		}
		if msg == "" {
			msg = "获取 115 二维码失败"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	c.mu.Lock()
	c.qrUID = resp.Data.UID
	c.qrTime = resp.Data.Time
	c.qrSign = resp.Data.Sign
	c.qrExpire = time.Now().Add(2 * time.Minute).Unix()
	c.mu.Unlock()
	return &LoginResult{
		VerificationURI: fmt.Sprintf(apiQRImage, url.QueryEscape(resp.Data.UID)),
		UserCode:        "请用 115 App 或 115 浏览器扫码",
		ExpiresIn:       120,
		Interval:        2,
	}, nil
}

// PollLogin 轮询扫码；确认后换 Cookie 并落盘。
func (c *Client) PollLogin(ctx context.Context) (*PollResult, error) {
	c.mu.RLock()
	uid, tm, sign, exp := c.qrUID, c.qrTime, c.qrSign, c.qrExpire
	c.mu.RUnlock()
	if uid == "" || time.Now().Unix() > exp {
		return &PollResult{Status: "NO_LOGIN"}, nil
	}
	q := url.Values{
		"uid":  {uid},
		"time": {fmt.Sprintf("%d", tm)},
		"sign": {sign},
	}
	b, err := c.do(ctx, http.MethodGet, apiQRStatus+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var st struct {
		State int `json:"state"`
		Data  struct {
			Status int    `json:"status"`
			Msg    string `json:"msg"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	switch st.Data.Status {
	case 0:
		return &PollResult{Status: "WAITING"}, nil
	case 1:
		return &PollResult{Status: "SCANNED"}, nil
	case -1:
		return &PollResult{Status: "EXPIRED"}, nil
	case -2:
		return &PollResult{Status: "CANCELED"}, nil
	case 2:
		// 用 android 换较长寿命 Cookie（web/linux/mac/windows 已下架）
		ck, err := c.finishQR(ctx, uid, "android")
		if err != nil {
			return nil, err
		}
		c.saveCookie(ck)
		c.mu.Lock()
		c.qrUID = ""
		c.mu.Unlock()
		return &PollResult{Status: "AUTHORIZATION_SUCCESS", LoggedIn: true}, nil
	default:
		return &PollResult{Status: fmt.Sprintf("STATUS_%d", st.Data.Status)}, nil
	}
}

func (c *Client) finishQR(ctx context.Context, uid, app string) (string, error) {
	form := url.Values{"account": {uid}, "app": {app}}
	b, err := c.do(ctx, http.MethodPost, fmt.Sprintf(apiQRLogin, app), form)
	if err != nil {
		return "", err
	}
	var resp struct {
		State int `json:"state"`
		Data  struct {
			Cookie struct {
				UID  string `json:"UID"`
				CID  string `json:"CID"`
				SEID string `json:"SEID"`
				KID  string `json:"KID"`
			} `json:"cookie"`
		} `json:"data"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return "", fmt.Errorf("解析登录结果: %w", err)
	}
	if resp.State != 1 || resp.Data.Cookie.UID == "" {
		msg := resp.Error
		if msg == "" {
			msg = resp.Message
		}
		if msg == "" {
			msg = "扫码登录失败"
		}
		return "", fmt.Errorf("%s", msg)
	}
	ck := fmt.Sprintf("UID=%s; CID=%s; SEID=%s", resp.Data.Cookie.UID, resp.Data.Cookie.CID, resp.Data.Cookie.SEID)
	if resp.Data.Cookie.KID != "" {
		ck += "; KID=" + resp.Data.Cookie.KID
	}
	return ck, nil
}

// LoggedIn 是否已有 Cookie。
func (c *Client) LoggedIn() bool {
	return c.cookieHeader() != ""
}
