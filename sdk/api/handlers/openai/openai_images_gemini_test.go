package openai

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/tidwall/gjson"
)

func TestGeminiImagesModelValidation(t *testing.T) {
	for _, model := range []string{
		"gemini-3.1-flash-image", "gemini-3.1-flash-image-preview",
		"gemini-3-pro-image-preview", "gemini-2.5-flash-image",
		"channel/gemini-3.1-flash-image", "models/gemini-3.1-flash-image",
	} {
		if !isSupportedImagesModel(model) {
			t.Errorf("model %q was rejected", model)
		}
	}
	if isSupportedImagesModel("gemini-3.1-pro") {
		t.Fatal("chat-only model must remain unsupported")
	}
	clientID := t.Name()
	registry.GetGlobalRegistry().RegisterClient(clientID, "gemini", []*registry.ModelInfo{
		{ID: "banana-alias", Type: "gemini", SupportedOutputModalities: []string{"TEXT", "IMAGE"}},
		{ID: "gemini-compat/gemini-3.1-flash-image", Type: registry.OpenAIImageModelType},
	})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(clientID) })
	if !isGeminiImagesModel("banana-alias") {
		t.Fatal("registered Gemini image alias must be supported")
	}
	if isGeminiImagesModel("gemini-compat/gemini-3.1-flash-image") {
		t.Fatal("configured OpenAI image endpoint must retain direct forwarding")
	}
}

func TestGeminiImagesEndpointsReachNativeUpstream(t *testing.T) {
	for _, provider := range []string{"gemini", "antigravity"} {
		for _, tc := range []struct {
			name       string
			path       string
			body       string
			multipart  bool
			wantImages int64
			wantURL    bool
			wantStream bool
		}{
			{name: "generation", path: imagesGenerationsPath, body: `{"model":"gemini-3.1-flash-image","prompt":"draw a cat","size":"1536x1024","resolution":"2k","generationConfig":{"temperature":0.5}}`},
			{name: "generation references", path: imagesGenerationsPath, body: `{"model":"gemini-3.1-flash-image","prompt":"draw a cat","image":["data:image/png;base64,QUJD","data:image/jpeg;base64,REVG"]}`, wantImages: 2},
			{name: "edit", path: imagesEditsPath, body: `{"model":"gemini-3.1-flash-image","prompt":"draw a cat","image":"data:image/png;base64,QUJD"}`, wantImages: 1},
			{name: "edit image objects", path: imagesEditsPath, body: `{"model":"gemini-3.1-flash-image","prompt":"draw a cat","images":[{"image_url":"data:image/png;base64,QUJD"}]}`, wantImages: 1},
			{name: "url", path: imagesGenerationsPath, body: `{"model":"gemini-3.1-flash-image","prompt":"draw a cat","response_format":"url"}`, wantURL: true},
			{name: "stream", path: imagesGenerationsPath, body: `{"model":"gemini-3.1-flash-image","prompt":"draw a cat","stream":true}`, wantStream: true},
			{name: "edit stream", path: imagesEditsPath, body: `{"model":"gemini-3.1-flash-image","prompt":"draw a cat","image":"data:image/png;base64,QUJD","stream":true,"response_format":"url"}`, wantImages: 1, wantStream: true, wantURL: true},
			{name: "multipart", path: imagesEditsPath, multipart: true, wantImages: 1},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				requests := make(chan []byte, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					payload, errRead := io.ReadAll(r.Body)
					if errRead != nil {
						t.Error(errRead)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					requests <- payload
					response := `{"candidates":[{"content":{"role":"model","parts":[{"text":"Here is the image"},{"inlineData":{"mimeType":"image/png","data":"QUJD"}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":20,"totalTokenCount":30}}`
					if provider == "antigravity" {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"response\":"+response+"}\n\n")
					} else {
						w.Header().Set("Content-Type", "application/json")
						if r.URL.Path != "/v1beta/models/gemini-3.1-flash-image:generateContent" {
							t.Errorf("unexpected native path %s", r.URL.Path)
						}
						_, _ = io.WriteString(w, response)
					}
				}))
				defer server.Close()
				cfg := &config.Config{Payload: config.PayloadConfig{
					Override: []config.PayloadRule{{
						Models: []config.PayloadModelRule{{Name: "gemini-3.1-flash-image", Protocol: provider}},
						Params: map[string]any{"generationConfig.imageConfig.imageSize": "4K"},
					}},
					Filter: []config.PayloadFilterRule{{
						Models: []config.PayloadModelRule{{Name: "gemini-3.1-flash-image", Protocol: provider}},
						Params: []string{"generationConfig.temperature"},
					}},
				}}
				manager := coreauth.NewManager(nil, nil, nil)
				if provider == "antigravity" {
					manager.RegisterExecutor(runtimeexecutor.NewAntigravityExecutor(cfg))
				} else {
					manager.RegisterExecutor(runtimeexecutor.NewGeminiExecutor(cfg))
				}
				auth := &coreauth.Auth{
					ID: t.Name(), Provider: provider, Status: coreauth.StatusActive, ProxyURL: "direct",
					Attributes: map[string]string{"api_key": "test-key", "base_url": server.URL},
					Metadata:   map[string]any{"access_token": "test-token", "expired": time.Now().Add(time.Hour).Format(time.RFC3339), "project_id": "test-project"},
				}
				if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
					t.Fatal(errRegister)
				}
				registry.GetGlobalRegistry().RegisterClient(auth.ID, provider, []*registry.ModelInfo{{ID: "gemini-3.1-flash-image", Type: provider}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
				handler := NewOpenAIAPIHandler(handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager))
				var body io.Reader = strings.NewReader(tc.body)
				contentType := "application/json"
				if tc.multipart {
					var buffer bytes.Buffer
					writer := multipart.NewWriter(&buffer)
					for key, value := range map[string]string{"model": "gemini-3.1-flash-image", "prompt": "draw a cat", "n": "1"} {
						if errWrite := writer.WriteField(key, value); errWrite != nil {
							t.Fatal(errWrite)
						}
					}
					part, errCreate := writer.CreateFormFile("image[]", "reference.png")
					if errCreate != nil {
						t.Fatal(errCreate)
					}
					_, _ = part.Write([]byte("\x89PNG\r\n\x1a\nreference"))
					if errClose := writer.Close(); errClose != nil {
						t.Fatal(errClose)
					}
					contentType, body = writer.FormDataContentType(), &buffer
				}
				endpointHandler := gin.HandlerFunc(handler.ImagesGenerations)
				if tc.path == imagesEditsPath {
					endpointHandler = handler.ImagesEdits
				}
				resp := performImagesEndpointRequest(t, tc.path, contentType, body, endpointHandler)
				if resp.Code != http.StatusOK {
					t.Fatalf("status = %d: %s", resp.Code, resp.Body.String())
				}
				payload := <-requests
				if provider == "antigravity" {
					payload = []byte(gjson.GetBytes(payload, "request").Raw)
				}
				if got := gjson.GetBytes(payload, "contents.0.parts.#").Int(); got != tc.wantImages+1 {
					t.Fatalf("native parts = %d, want %d: %s", got, tc.wantImages+1, payload)
				}
				if got := gjson.GetBytes(payload, "generationConfig.responseModalities.1").String(); got != "IMAGE" {
					t.Fatalf("image modality missing: %s", payload)
				}
				if got := gjson.GetBytes(payload, "generationConfig.imageConfig.imageSize").String(); got != "4K" {
					t.Fatalf("payload override was lost: %s", payload)
				}
				if gjson.GetBytes(payload, "generationConfig.temperature").Exists() {
					t.Fatalf("payload filter was undone: %s", payload)
				}
				if tc.name == "generation" && gjson.GetBytes(payload, "generationConfig.imageConfig.aspectRatio").String() != "3:2" {
					t.Fatalf("aspect ratio missing: %s", payload)
				}
				response := resp.Body.Bytes()
				if tc.wantStream {
					prefix := "image_generation"
					if tc.path == imagesEditsPath {
						prefix = "image_edit"
					}
					if !strings.Contains(resp.Body.String(), "event: "+prefix+".completed") {
						t.Fatalf("image completion event missing: %s", response)
					}
					for _, line := range strings.Split(resp.Body.String(), "\n") {
						if strings.HasPrefix(line, "data: ") {
							response = []byte(strings.TrimPrefix(line, "data: "))
						}
					}
				} else {
					if gjson.GetBytes(response, "usage.total_tokens").Int() != 30 {
						t.Fatalf("usage missing: %s", response)
					}
					response = []byte(gjson.GetBytes(response, "data.0").Raw)
				}
				field, want := "b64_json", "QUJD"
				if tc.wantURL {
					field, want = "url", "data:image/png;base64,QUJD"
				}
				if got := gjson.GetBytes(response, field).String(); got != want {
					t.Fatalf("%s = %q, want %q: %s", field, got, want, response)
				}
			})
		}
	}
}

func TestGeminiImagesRejectInvalidRequests(t *testing.T) {
	handler := &OpenAIAPIHandler{}
	for _, tc := range []struct {
		path string
		body string
	}{
		{imagesEditsPath, `{"model":"gemini-3.1-flash-image","prompt":"edit"}`},
		{imagesGenerationsPath, `{"model":"gemini-3.1-flash-image","prompt":"draw","n":2}`},
		{imagesEditsPath, `{"model":"gemini-3.1-flash-image","prompt":"edit","image":"data:image/png;base64,invalid!"}`},
		{imagesEditsPath, `{"model":"gemini-3.1-flash-image","prompt":"edit","image":"https://example.com/image.png"}`},
		{imagesEditsPath, `{"model":"gemini-3.1-flash-image","prompt":"edit","image":"data:image/png;base64,QUJD","mask":{"image_url":"data:image/png;base64,QUJD"}}`},
	} {
		endpointHandler := gin.HandlerFunc(handler.ImagesGenerations)
		if tc.path == imagesEditsPath {
			endpointHandler = handler.ImagesEdits
		}
		resp := performImagesEndpointRequest(t, tc.path, "application/json", strings.NewReader(tc.body), endpointHandler)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", resp.Code, resp.Body.String())
		}
	}
}

func TestNormalizeGeminiImagesResponse(t *testing.T) {
	payload := []byte(`{"candidates":[{"content":{"parts":[{"thought":true,"inlineData":{"mimeType":"image/png","data":"hidden"}},{"inline_data":{"mime_type":"image/webp","data":"QUJD"}},{"inlineData":{"mimeType":"audio/wav","data":"REVG"}}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":20,"thoughtsTokenCount":5,"totalTokenCount":35}}`)
	normalized, err := normalizeGeminiImagesResponse(payload)
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(normalized, "data.#").Int() != 1 || gjson.GetBytes(normalized, "usage.output_tokens").Int() != 25 {
		t.Fatalf("unexpected normalized response: %s", normalized)
	}
	response, err := buildImagesAPIResponseFromXAI(normalized, "url")
	if err != nil || gjson.GetBytes(response, "data.0.url").String() != "data:image/webp;base64,QUJD" {
		t.Fatalf("image MIME type was lost: %s, %v", response, err)
	}
	for _, payload := range []string{`invalid`, `{"candidates":[{"content":{"parts":[{"text":"cannot generate"}]}}]}`} {
		if _, err := normalizeGeminiImagesResponse([]byte(payload)); err == nil {
			t.Fatalf("expected error for %s", payload)
		}
	}
}
