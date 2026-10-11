package zcode

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const (
	DefaultOAuthOrigin    = "https://zcode.z.ai"
	DefaultBusinessOrigin = "https://bigmodel.cn"
	DefaultAPIBaseURL     = "https://open.bigmodel.cn/api/coding/paas/v4"
	ClientVersion         = "3.14.5"
	MaxLoginDuration      = 5 * time.Minute
)

type Client struct {
	httpClient     *http.Client
	oauthOrigin    string
	businessOrigin string
	now            func() time.Time
}

func NewClient(cfg *config.Config, proxyURL string) *Client {
	var sdkCfg config.SDKConfig
	if cfg != nil {
		sdkCfg = cfg.SDKConfig
	}
	if strings.TrimSpace(proxyURL) != "" {
		sdkCfg.ProxyURL = strings.TrimSpace(proxyURL)
	}
	return &Client{
		httpClient:     util.SetProxy(&sdkCfg, &http.Client{Timeout: 30 * time.Second}),
		oauthOrigin:    DefaultOAuthOrigin,
		businessOrigin: DefaultBusinessOrigin,
		now:            time.Now,
	}
}

type LoginFlow struct {
	FlowID              string `json:"flow_id"`
	AuthorizeURL        string `json:"authorize_url"`
	ExpiresAt           int64  `json:"expires_at"`
	PollIntervalSeconds int    `json:"poll_interval_sec"`
	State               string `json:"-"`
	pollToken           string
}

type LoginResult struct {
	Status   string `json:"status"`
	Token    string `json:"token"`
	BigModel struct {
		AccessToken string `json:"access_token"`
	} `json:"bigmodel"`
	User struct {
		ID    string `json:"user_id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"user"`
}

var errConnection = errors.New("ZCode upstream connection failed")

type upstreamError struct{ status int }

func (e upstreamError) Error() string {
	return fmt.Sprintf("ZCode upstream returned HTTP %d", e.status)
}

// Business responses may use either numeric or string success codes.
func (c *Client) request(ctx context.Context, method, endpoint, authorization string, body any, result any) error {
	var payload []byte
	if body != nil {
		var errMarshal error
		payload, errMarshal = json.Marshal(body)
		if errMarshal != nil {
			return fmt.Errorf("encode ZCode request: %w", errMarshal)
		}
	}
	req, errRequest := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if errRequest != nil {
		return errors.New("invalid ZCode request URL")
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, errDo := c.httpClient.Do(req)
	if errDo != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errConnection
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return upstreamError{status: resp.StatusCode}
	}
	var envelope struct {
		Code json.RawMessage `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if errDecode := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&envelope); errDecode != nil {
		return errors.New("invalid ZCode upstream response")
	}
	code := strings.Trim(string(envelope.Code), "\"")
	if code != "0" && code != "200" {
		return errors.New("ZCode upstream rejected the request; check the account authorization and Coding Plan subscription")
	}
	if result != nil {
		if errDecode := json.Unmarshal(envelope.Data, result); errDecode != nil {
			return errors.New("invalid ZCode upstream response data")
		}
	}
	return nil
}

func (c *Client) StartLogin(ctx context.Context) (*LoginFlow, error) {
	secret := make([]byte, 32)
	if _, errRandom := rand.Read(secret); errRandom != nil {
		return nil, fmt.Errorf("generate ZCode polling token: %w", errRandom)
	}
	flow := &LoginFlow{pollToken: hex.EncodeToString(secret)}
	if errStart := c.request(ctx, http.MethodPost, c.oauthOrigin+"/api/v1/oauth/cli/init", "Bearer "+flow.pollToken, map[string]string{"provider": "bigmodel"}, flow); errStart != nil {
		return nil, errStart
	}
	u, errURL := url.Parse(flow.AuthorizeURL)
	if errURL != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, errors.New("invalid ZCode authorization URL")
	}
	flow.State = strings.TrimSpace(u.Query().Get("state"))
	remaining := time.Unix(flow.ExpiresAt, 0).Sub(c.now())
	if strings.TrimSpace(flow.FlowID) == "" || flow.State == "" || remaining <= 0 || flow.PollIntervalSeconds < 1 || time.Duration(flow.PollIntervalSeconds)*time.Second >= remaining {
		return nil, errors.New("invalid ZCode login flow")
	}
	return flow, nil
}

func (c *Client) WaitForLogin(ctx context.Context, flow *LoginFlow) (*LoginResult, error) {
	if flow == nil || flow.pollToken == "" || flow.FlowID == "" || flow.PollIntervalSeconds < 1 {
		return nil, errors.New("missing ZCode login flow")
	}
	deadline := time.Unix(flow.ExpiresAt, 0)
	if limit := c.now().Add(MaxLoginDuration); deadline.After(limit) {
		deadline = limit
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	for {
		var result LoginResult
		errPoll := c.request(ctx, http.MethodGet, c.oauthOrigin+"/api/v1/oauth/cli/poll/"+url.PathEscape(flow.FlowID), "Bearer "+flow.pollToken, nil, &result)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errPoll != nil {
			var upstream upstreamError
			if errors.As(errPoll, &upstream) && upstream.status >= 400 && upstream.status < 500 && upstream.status != 408 && upstream.status != 429 {
				return nil, errPoll
			}
			if !errors.As(errPoll, &upstream) && !errors.Is(errPoll, errConnection) {
				return nil, errPoll
			}
		} else {
			switch result.Status {
			case "ready":
				if strings.TrimSpace(result.BigModel.AccessToken) == "" || strings.TrimSpace(result.User.ID) == "" {
					return nil, errors.New("ZCode authorization did not return account credentials")
				}
				return &result, nil
			case "pending":
			case "failed":
				return nil, errors.New("ZCode authorization failed")
			default:
				return nil, errors.New("invalid ZCode authorization status")
			}
		}
		timer := time.NewTimer(time.Duration(flow.PollIntervalSeconds) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

type ProjectLocation struct {
	OrganizationID string `json:"organization_id"`
	ProjectID      string `json:"project_id"`
	APIKeyID       string `json:"api_key_id"`
}

type ProjectToken struct {
	Enable      *bool  `json:"enable"`
	AccessToken string `json:"accessToken"`
	TokenType   string `json:"tokenType"`
	ExpiresIn   int64  `json:"expiresIn"`
	ExpiresAt   int64  `json:"expiresAt"`
}

func (c *Client) ResolveProject(ctx context.Context, loginToken string) (*ProjectLocation, error) {
	loginToken = strings.TrimSpace(loginToken)
	if loginToken == "" {
		return nil, errors.New("missing ZCode account authorization")
	}
	type project struct {
		ID   string          `json:"projectId"`
		Name string          `json:"projectName"`
		Type json.RawMessage `json:"projectType"`
	}
	var profile struct {
		Organizations []struct {
			ID       string    `json:"organizationId"`
			Name     string    `json:"organizationName"`
			Projects []project `json:"projects"`
		} `json:"organizations"`
	}
	if errProfile := c.request(ctx, http.MethodGet, c.businessOrigin+"/api/biz/customer/getCustomerInfo", loginToken, nil, &profile); errProfile != nil {
		return nil, errProfile
	}
	var selected *ProjectLocation
	preferredOrganization := false
	for _, organization := range profile.Organizations {
		var selectedProject *project
		for i := range organization.Projects {
			p := &organization.Projects[i]
			if strings.Trim(string(p.Type), "\"") == "2" || strings.TrimSpace(p.ID) == "" {
				continue
			}
			if selectedProject == nil || strings.Contains(p.Name, "默认项目") {
				selectedProject = p
			}
			if strings.Contains(p.Name, "默认项目") {
				break
			}
		}
		if organization.ID == "" || selectedProject == nil {
			continue
		}
		preferred := strings.Contains(organization.Name, "默认机构")
		if selected == nil || preferred && !preferredOrganization {
			selected = &ProjectLocation{OrganizationID: organization.ID, ProjectID: selectedProject.ID}
			preferredOrganization = preferred
		}
	}
	if selected == nil {
		return nil, errors.New("no personal Coding Plan project found; team subscriptions are not supported by this login")
	}
	endpoint := c.keyURL(*selected)
	var keys []struct {
		Name string `json:"name"`
		Key  string `json:"apiKey"`
	}
	if errKeys := c.request(ctx, http.MethodGet, endpoint, loginToken, nil, &keys); errKeys != nil {
		return nil, errKeys
	}
	for _, key := range keys {
		if key.Name == "zcode-api-key" && strings.TrimSpace(key.Key) != "" {
			selected.APIKeyID = key.Key
			break
		}
	}
	if selected.APIKeyID == "" {
		var key struct {
			Key string `json:"apiKey"`
		}
		if errCreate := c.request(ctx, http.MethodPost, endpoint, loginToken, map[string]any{"name": "zcode-api-key", "usageScene": 1}, &key); errCreate != nil {
			return nil, errCreate
		}
		selected.APIKeyID = strings.TrimSpace(key.Key)
	}
	if selected.APIKeyID == "" {
		return nil, errors.New("Coding Plan did not return a project key")
	}
	return selected, nil
}

func (c *Client) keyURL(location ProjectLocation) string {
	return c.businessOrigin + "/api/biz/v1/organization/" + url.PathEscape(location.OrganizationID) + "/projects/" + url.PathEscape(location.ProjectID) + "/api_keys"
}

func (c *Client) IssueProjectToken(ctx context.Context, loginToken string, location ProjectLocation) (*ProjectToken, error) {
	if strings.TrimSpace(loginToken) == "" || location.OrganizationID == "" || location.ProjectID == "" || location.APIKeyID == "" {
		return nil, errors.New("missing Coding Plan project authorization")
	}
	var token ProjectToken
	if errToken := c.request(ctx, http.MethodPost, c.keyURL(location)+"/"+url.PathEscape(location.APIKeyID)+"/access_tokens", "Bearer "+strings.TrimSpace(loginToken), map[string]string{"clientType": "zcode", "clientVersion": ClientVersion}, &token); errToken != nil {
		return nil, errToken
	}
	if token.Enable != nil && !*token.Enable {
		return nil, errors.New("Coding Plan project authorization is disabled")
	}
	if strings.TrimSpace(token.AccessToken) == "" || token.TokenType != "Bearer" || token.ExpiresIn <= 0 || token.ExpiresAt <= c.now().Unix() {
		return nil, errors.New("invalid Coding Plan project token")
	}
	if relative := c.now().Add(time.Duration(token.ExpiresIn) * time.Second).Unix(); token.ExpiresAt > relative {
		token.ExpiresAt = relative
	}
	return &token, nil
}

func (c *Client) CreateAuthRecord(ctx context.Context, login *LoginResult) (*coreauth.Auth, error) {
	if login == nil || strings.TrimSpace(login.User.ID) == "" {
		return nil, errors.New("missing ZCode account identity")
	}
	location, errProject := c.ResolveProject(ctx, login.BigModel.AccessToken)
	if errProject != nil {
		return nil, errProject
	}
	token, errToken := c.IssueProjectToken(ctx, login.BigModel.AccessToken, *location)
	if errToken != nil {
		return nil, errToken
	}
	hash := sha256.Sum256([]byte(strings.TrimSpace(login.User.ID)))
	filename := "zcode-" + hex.EncodeToString(hash[:16]) + ".json"
	label := strings.TrimSpace(login.User.Name)
	if label == "" {
		label = strings.TrimSpace(login.User.Email)
	}
	if label == "" {
		label = "ZCode"
	}
	auth := &coreauth.Auth{ID: filename, FileName: filename, Provider: "zcode", Label: label, Status: coreauth.StatusActive, Metadata: map[string]any{
		"type": "zcode", "auth_kind": "oauth", "label": label, "email": login.User.Email, "user_id": login.User.ID,
		"zcode_jwt": strings.TrimSpace(login.Token), "business_access_token": login.BigModel.AccessToken, "organization_id": location.OrganizationID, "project_id": location.ProjectID, "api_key_id": location.APIKeyID,
		"base_url": DefaultAPIBaseURL, "plan_type": "individual-coding-plan",
	}}
	ApplyProjectToken(auth, token, c.now())
	return auth, nil
}

func ApplyProjectToken(auth *coreauth.Auth, token *ProjectToken, now time.Time) {
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["access_token"] = token.AccessToken
	auth.Metadata["expired"] = time.Unix(token.ExpiresAt, 0).UTC().Format(time.RFC3339)
	auth.Metadata["last_refresh"] = now.UTC().Format(time.RFC3339)
	auth.LastRefreshedAt = now
}
