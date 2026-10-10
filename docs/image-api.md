# Image API

`POST /v1/images/generations` and `POST /v1/images/edits` support GPT image
models, Grok image models, configured OpenAI-compatible image models, and
Gemini image models such as `gemini-3.1-flash-image`,
`gemini-3-pro-image-preview`, and `gemini-2.5-flash-image`.
Channel-prefixed model IDs use the normal credential routing rules.

## Gemini images

Gemini requests use the existing provider executors and native
`generateContent` payloads, including Antigravity's native transport. Responses
are converted to the OpenAI image response format rather than treating Gemini
as a GPT image-generation tool.

```json
{
  "model": "gemini-3.1-flash-image",
  "prompt": "Draw a watercolor cat",
  "response_format": "b64_json",
  "aspect_ratio": "3:2",
  "resolution": "2K"
}
```

- `response_format` defaults to `b64_json`; `url` returns an image data URL.
- `stream: true` returns SSE with `image_generation.completed` or
  `image_edit.completed` events when generation finishes. Partial images are
  not synthesized.
- Both endpoints accept reference images in `image` or `images`, as a string,
  an array of strings, or objects containing `image_url` / `image_url.url` / `url`.
  References must be base64 image data URLs. `/v1/images/edits` also accepts
  multipart image uploads in `image` or `image[]` and requires a reference image.
  Remote image URLs and file IDs are not fetched or resolved.
- `aspect_ratio` maps to `generationConfig.imageConfig.aspectRatio`.
  Recognized pixel dimensions in `size` also select the aspect ratio.
- `resolution` or `image_size` maps to `generationConfig.imageConfig.imageSize`;
  `size` values `512`, `1K`, `2K`, and `4K` also select resolution. The upstream
  model determines which ratios and resolutions it accepts.
- Native options can be supplied in a JSON `generationConfig` object.
  The adapter requests both text and image output; only image parts are returned.
- `n` may be omitted or set to `1`. Other counts and edit masks return HTTP 400.
  Describe edits in the prompt instead of using a mask. GPT-specific options
  such as `quality`, `background`, and `output_format` are not mapped to Gemini.
- User payload rules are applied by the selected executor after native request
  normalization. Override and filter rules retain their final precedence.
