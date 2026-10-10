package openai

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func isGeminiImagesModel(model string) bool {
	if isOpenAICompatImagesModel(model) {
		return false
	}
	base := imagesModelBase(model)
	if strings.HasPrefix(base, "gemini-") && strings.Contains(base, "-image") {
		return true
	}
	info := registry.LookupModelInfo(model)
	if info == nil {
		return false
	}
	switch info.Type {
	case "gemini", "gemini-cli", "antigravity", "vertex":
		for _, modality := range info.SupportedOutputModalities {
			if strings.EqualFold(modality, "image") {
				return true
			}
		}
	}
	return false
}

func collectGeminiImagesFromJSON(rawJSON []byte) []string {
	var images []string
	appendImage := func(image gjson.Result) {
		var url string
		if image.Type == gjson.String {
			url = image.String()
		} else {
			url = image.Get("image_url.url").String()
			if url == "" && image.Get("image_url").Type == gjson.String {
				url = image.Get("image_url").String()
			}
			if url == "" {
				url = image.Get("url").String()
			}
		}
		if url = strings.TrimSpace(url); url != "" {
			images = append(images, url)
		}
	}
	for _, field := range []string{"image", "images"} {
		value := gjson.GetBytes(rawJSON, field)
		if value.IsArray() {
			for _, image := range value.Array() {
				appendImage(image)
			}
		} else if value.Exists() {
			appendImage(value)
		}
	}
	return images
}

func geminiImagesAspectRatio(size string) string {
	if size == "" || strings.EqualFold(size, "auto") {
		return ""
	}
	if strings.Contains(size, ":") {
		return size
	}
	parts := strings.Split(strings.ToLower(size), "x")
	if len(parts) != 2 {
		return ""
	}
	width, errWidth := strconv.Atoi(parts[0])
	height, errHeight := strconv.Atoi(parts[1])
	if errWidth != nil || errHeight != nil || width <= 0 || height <= 0 {
		return ""
	}
	for _, ratio := range []string{"1:1", "1:4", "1:8", "2:3", "3:2", "3:4", "4:1", "4:3", "4:5", "5:4", "8:1", "9:16", "16:9", "21:9"} {
		values := strings.Split(ratio, ":")
		w, _ := strconv.Atoi(values[0])
		h, _ := strconv.Atoi(values[1])
		if width*h == height*w {
			return ratio
		}
	}
	return ""
}

func buildGeminiImagesRequest(rawJSON []byte, images []string) ([]byte, error) {
	if n := gjson.GetBytes(rawJSON, "n"); n.Exists() && n.Int() != 1 {
		return nil, fmt.Errorf("Gemini image generation supports n=1 only")
	}
	if mask := gjson.GetBytes(rawJSON, "mask"); mask.Exists() && mask.Raw != "null" && mask.String() != "" {
		return nil, fmt.Errorf("Gemini image editing does not support mask; describe the edit in prompt")
	}
	req := []byte(`{"contents":[{"role":"user","parts":[]}],"generationConfig":{"responseModalities":["TEXT","IMAGE"]}}`)
	text, _ := sjson.SetBytes([]byte(`{}`), "text", strings.TrimSpace(gjson.GetBytes(rawJSON, "prompt").String()))
	req, _ = sjson.SetRawBytes(req, "contents.0.parts.-1", text)
	for _, image := range images {
		mediaType, encoded, ok := strings.Cut(strings.TrimPrefix(image, "data:"), ";base64,")
		if !strings.HasPrefix(image, "data:") || !ok || !strings.HasPrefix(strings.ToLower(mediaType), "image/") || encoded == "" {
			return nil, fmt.Errorf("Gemini reference images must be base64 image data URLs (or uploaded files)")
		}
		if _, errDecode := base64.StdEncoding.DecodeString(encoded); errDecode != nil {
			return nil, fmt.Errorf("invalid reference image base64: %w", errDecode)
		}
		part := []byte(`{"inlineData":{}}`)
		part, _ = sjson.SetBytes(part, "inlineData.mimeType", mediaType)
		part, _ = sjson.SetBytes(part, "inlineData.data", encoded)
		req, _ = sjson.SetRawBytes(req, "contents.0.parts.-1", part)
	}
	if config := gjson.GetBytes(rawJSON, "generationConfig"); config.IsObject() {
		req, _ = sjson.SetRawBytes(req, "generationConfig", []byte(config.Raw))
		req, _ = sjson.SetRawBytes(req, "generationConfig.responseModalities", []byte(`["TEXT","IMAGE"]`))
	}
	aspectRatio := strings.TrimSpace(gjson.GetBytes(rawJSON, "aspect_ratio").String())
	size := strings.TrimSpace(gjson.GetBytes(rawJSON, "size").String())
	if aspectRatio == "" {
		aspectRatio = geminiImagesAspectRatio(size)
	}
	if aspectRatio != "" {
		req, _ = sjson.SetBytes(req, "generationConfig.imageConfig.aspectRatio", aspectRatio)
	}
	resolution := strings.TrimSpace(gjson.GetBytes(rawJSON, "resolution").String())
	if resolution == "" {
		resolution = strings.TrimSpace(gjson.GetBytes(rawJSON, "image_size").String())
	}
	if resolution == "" {
		switch strings.ToUpper(size) {
		case "512", "1K", "2K", "4K":
			resolution = size
		}
	}
	if resolution != "" {
		req, _ = sjson.SetBytes(req, "generationConfig.imageConfig.imageSize", strings.ToUpper(resolution))
	}
	return req, nil
}

func normalizeGeminiImagesResponse(payload []byte) ([]byte, error) {
	if !gjson.ValidBytes(payload) {
		return nil, fmt.Errorf("upstream returned invalid Gemini image response JSON")
	}
	out := []byte(`{"data":[]}`)
	for _, candidate := range gjson.GetBytes(payload, "candidates").Array() {
		for _, part := range candidate.Get("content.parts").Array() {
			if part.Get("thought").Bool() {
				continue
			}
			image := part.Get("inlineData")
			if !image.Exists() {
				image = part.Get("inline_data")
			}
			mimeType := image.Get("mimeType").String()
			if mimeType == "" {
				mimeType = image.Get("mime_type").String()
			}
			if !strings.HasPrefix(strings.ToLower(mimeType), "image/") || image.Get("data").String() == "" {
				continue
			}
			item := []byte(`{}`)
			item, _ = sjson.SetBytes(item, "b64_json", image.Get("data").String())
			item, _ = sjson.SetBytes(item, "mime_type", mimeType)
			out, _ = sjson.SetRawBytes(out, "data.-1", item)
		}
	}
	if gjson.GetBytes(out, "data.#").Int() == 0 {
		return nil, fmt.Errorf("upstream did not return image output")
	}
	if usage := gjson.GetBytes(payload, "usageMetadata"); usage.IsObject() {
		input := usage.Get("promptTokenCount").Int()
		output := usage.Get("candidatesTokenCount").Int() + usage.Get("thoughtsTokenCount").Int()
		total := usage.Get("totalTokenCount").Int()
		if total == 0 {
			total = input + output
		}
		out, _ = sjson.SetBytes(out, "usage.input_tokens", input)
		out, _ = sjson.SetBytes(out, "usage.output_tokens", output)
		out, _ = sjson.SetBytes(out, "usage.total_tokens", total)
	}
	return out, nil
}

func (h *OpenAIAPIHandler) handleGeminiImages(c *gin.Context, rawJSON []byte, imageModel, responseFormat, streamPrefix string, stream bool, images []string) {
	if streamPrefix == "image_edit" && len(images) == 0 {
		c.JSON(http.StatusBadRequest, handlers.ErrorResponse{Error: handlers.ErrorDetail{
			Message: "Invalid request: image is required", Type: "invalid_request_error",
		}})
		return
	}
	req, errBuild := buildGeminiImagesRequest(rawJSON, images)
	if errBuild != nil {
		c.JSON(http.StatusBadRequest, handlers.ErrorResponse{Error: handlers.ErrorDetail{
			Message: "Invalid request: " + errBuild.Error(), Type: "invalid_request_error",
		}})
		return
	}
	if stream {
		h.streamImagesWithProtocol(c, req, imageModel, responseFormat, streamPrefix, "gemini")
		return
	}
	h.collectImagesWithProtocol(c, req, imageModel, responseFormat, "gemini")
}
