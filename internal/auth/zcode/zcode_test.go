package zcode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Client{httpClient: server.Client(), oauthOrigin: server.URL, businessOrigin: server.URL, now: time.Now}
}

func reply(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
}

func TestLoginFlowAndPollingSecret(t *testing.T) {
	var secret string
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/oauth/cli/init":
			if r.Method != http.MethodPost {
				t.Errorf("method = %s", r.Method)
			}
			var body map[string]string
			if errDecode := json.NewDecoder(r.Body).Decode(&body); errDecode != nil {
				t.Error(errDecode)
			}
			if body["provider"] != "bigmodel" {
				t.Errorf("provider = %s", body["provider"])
			}
			secret = r.Header.Get("Authorization")
			if !strings.HasPrefix(secret, "Bearer ") || len(secret) != 71 {
				t.Errorf("invalid polling secret")
			}
			reply(w, map[string]any{"flow_id": "flow-1", "authorize_url": "https://bigmodel.cn/login?state=state-1", "expires_at": time.Now().Add(time.Minute).Unix(), "poll_interval_sec": 1})
		case "/api/v1/oauth/cli/poll/flow-1":
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != secret {
				t.Error("poll did not reuse init authorization")
			}
			reply(w, map[string]any{"status": "ready", "token": "zcode-jwt", "bigmodel": map[string]string{"access_token": "business-token"}, "user": map[string]string{"user_id": "user-1"}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	flow, errStart := client.StartLogin(context.Background())
	if errStart != nil {
		t.Fatal(errStart)
	}
	if flow.State != "state-1" {
		t.Fatalf("state = %s", flow.State)
	}
	result, errWait := client.WaitForLogin(context.Background(), flow)
	if errWait != nil {
		t.Fatal(errWait)
	}
	if result.BigModel.AccessToken != "business-token" || result.Token != "zcode-jwt" {
		t.Error("wrong account credential")
	}
	encoded, _ := json.Marshal(flow)
	if strings.Contains(string(encoded), secret[7:]) {
		t.Error("polling secret exposed in JSON")
	}
}

func TestPollingCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]string{"status": "pending"})
		cancel()
	})
	_, errWait := client.WaitForLogin(ctx, &LoginFlow{FlowID: "pending", pollToken: "secret", ExpiresAt: time.Now().Add(time.Minute).Unix(), PollIntervalSeconds: 60})
	if !errors.Is(errWait, context.Canceled) {
		t.Fatalf("error = %v", errWait)
	}
}

func TestPollingTerminalErrorsAreSanitized(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("secret-login-token"))
			})
			_, errWait := client.WaitForLogin(context.Background(), &LoginFlow{FlowID: "denied", pollToken: "secret", ExpiresAt: time.Now().Add(time.Minute).Unix(), PollIntervalSeconds: 1})
			if errWait == nil || strings.Contains(errWait.Error(), "secret-login-token") {
				t.Fatalf("error = %v", errWait)
			}
		})
	}
}

func TestCreateAuthRecordSelectsPersonalProjectAndSeparatesTokens(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	for _, createKey := range []bool{false, true} {
		t.Run(map[bool]string{false: "reuse key", true: "create key"}[createKey], func(t *testing.T) {
			created := false
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				expectedAuth := "business-token"
				if strings.HasSuffix(r.URL.Path, "/access_tokens") {
					expectedAuth = "Bearer business-token"
				}
				if r.Header.Get("Authorization") != expectedAuth {
					t.Errorf("wrong authorization for %s", r.URL.Path)
				}
				switch r.URL.Path {
				case "/api/biz/customer/getCustomerInfo":
					reply(w, map[string]any{"organizations": []any{
						map[string]any{"organizationId": "team", "organizationName": "team", "projects": []any{map[string]any{"projectId": "team-project", "projectType": 2}}},
						map[string]any{"organizationId": "other", "organizationName": "other", "projects": []any{map[string]any{"projectId": "other-project", "projectType": 1}}},
						map[string]any{"organizationId": "personal", "organizationName": "默认机构", "projects": []any{map[string]any{"projectId": "secondary", "projectType": 1}, map[string]any{"projectId": "default", "projectName": "默认项目", "projectType": 1}}},
					}})
				case "/api/biz/v1/organization/personal/projects/default/api_keys":
					if r.Method == http.MethodGet {
						if createKey {
							reply(w, []any{})
						} else {
							reply(w, []any{map[string]string{"name": "zcode-api-key", "apiKey": "key-id"}})
						}
					} else {
						created = true
						var body map[string]any
						_ = json.NewDecoder(r.Body).Decode(&body)
						if body["name"] != "zcode-api-key" || body["usageScene"] != float64(1) {
							t.Errorf("key body = %v", body)
						}
						reply(w, map[string]string{"apiKey": "key-id"})
					}
				case "/api/biz/v1/organization/personal/projects/default/api_keys/key-id/access_tokens":
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					if r.Method != http.MethodPost || body["clientType"] != "zcode" || body["clientVersion"] != ClientVersion {
						t.Errorf("token request = %v", body)
					}
					reply(w, map[string]any{"accessToken": "inference-token", "tokenType": "Bearer", "expiresIn": 600, "expiresAt": now.Add(time.Hour).Unix()})
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			client.now = func() time.Time { return now }
			login := &LoginResult{}
			login.User.ID = "user-1"
			login.User.Name = "Account"
			login.BigModel.AccessToken = "business-token"
			login.Token = "zcode-jwt"
			auth, errCreate := client.CreateAuthRecord(context.Background(), login)
			if errCreate != nil {
				t.Fatal(errCreate)
			}
			if created != createKey {
				t.Errorf("created = %v", created)
			}
			if auth.Provider != "zcode" || auth.Metadata["auth_kind"] != "oauth" || auth.Metadata["access_token"] != "inference-token" || auth.Metadata["business_access_token"] != "business-token" || auth.Metadata["zcode_jwt"] != "zcode-jwt" {
				t.Fatalf("incorrect credential fields")
			}
			expiration, ok := auth.ExpirationTime()
			if !ok || !expiration.Equal(now.Add(10*time.Minute)) {
				t.Errorf("expiration = %v", expiration)
			}
			again, errAgain := client.CreateAuthRecord(context.Background(), login)
			if errAgain != nil || auth.FileName != again.FileName {
				t.Fatalf("unstable identity: %v", errAgain)
			}
			login.User.ID = "user-2"
			other, errOther := client.CreateAuthRecord(context.Background(), login)
			if errOther != nil || auth.FileName == other.FileName {
				t.Fatalf("account identities collide: %v", errOther)
			}
		})
	}
}

func TestIssueProjectTokenValidation(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	cases := []struct {
		name  string
		token ProjectToken
		valid bool
	}{
		{"valid", ProjectToken{AccessToken: "token", TokenType: "Bearer", ExpiresIn: 60, ExpiresAt: now.Add(time.Minute).Unix()}, true},
		{"disabled", ProjectToken{Enable: func() *bool { v := false; return &v }(), AccessToken: "token", TokenType: "Bearer", ExpiresIn: 60, ExpiresAt: now.Add(time.Minute).Unix()}, false},
		{"expired", ProjectToken{AccessToken: "token", TokenType: "Bearer", ExpiresIn: 60, ExpiresAt: now.Unix()}, false},
		{"missing", ProjectToken{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) { reply(w, tc.token) })
			client.now = func() time.Time { return now }
			_, errToken := client.IssueProjectToken(context.Background(), "login", ProjectLocation{OrganizationID: "org", ProjectID: "project", APIKeyID: "key"})
			if (errToken == nil) != tc.valid {
				t.Fatalf("error = %v", errToken)
			}
		})
	}
}
