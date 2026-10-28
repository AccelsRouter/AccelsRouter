package plugins_test

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The OpenAI first-frame reference arrives as a JSON URL or as a multipart
// upload; both must reach the TokenDance (Ark wire format) content list, where
// an upload is inlined by the host as a Base64 data URL.
func TestTokenDanceVideoInputReference(t *testing.T) {
	source, err := builtinplugins.Source("tokendance")
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "tokendance"})
	require.NoError(t, err)
	const model = "seedance-2-5"

	decode := func(body map[string]any) (map[string]any, error) {
		t.Helper()
		value, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_video", "decodeRequest"}, map[string]any{"model": model, "body": body})
		if err != nil {
			return nil, err
		}
		return alibabaObject(t, value), nil
	}
	submit := func(decoded map[string]any) []any {
		t.Helper()
		driver := map[string]any{"upstreamModel": model, "model": model, "action": decoded["action"], "requestBody": decoded["requestBody"], "baseUrl": "https://tokendance.example", "apiKey": "k"}
		value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", driver)
		require.NoError(t, err)
		return alibabaObject(t, value)["body"].(map[string]any)["content"].([]any)
	}

	decoded, err := decode(map[string]any{"kind": "json", "value": map[string]any{"model": model, "prompt": "a cat", "input_reference": "https://cdn.example/first.png", "seconds": "5"}})
	require.NoError(t, err)
	assert.Equal(t, "image_to_video", decoded["action"])
	assert.Equal(t, []any{
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://cdn.example/first.png"}, "role": "first_frame"},
		map[string]any{"type": "text", "text": "a cat"},
	}, submit(decoded))

	placeholder := map[string]any{"__fileRef": "request_file:input_reference", "encoding": "dataUrl", "mimeType": "image/jpeg", "maxBytes": float64(31457280)}
	decoded, err = decode(map[string]any{"kind": "multipart",
		"fields": map[string][]string{"model": {model}, "prompt": {"a cat"}, "seconds": {"5"}, "size": {"1280x720"}, "metadata": {`{"ratio":"16:9"}`}},
		"files":  []any{map[string]any{"ref": "request_file:input_reference", "field": "input_reference", "filename": "first.jpg", "mimeType": "IMAGE/JPEG", "size": 10}},
	})
	require.NoError(t, err)
	assert.Equal(t, "image_to_video", decoded["action"])
	requestBody := decoded["requestBody"].(map[string]any)
	assert.Equal(t, float64(5), requestBody["seconds"])
	assert.Equal(t, map[string]any{"ratio": "16:9", "resolution": "720p"}, requestBody["metadata"])
	assert.Equal(t, []any{
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": placeholder}, "role": "first_frame"},
		map[string]any{"type": "text", "text": "a cat"},
	}, submit(decoded), "the placeholder reaches the upstream body for the host to inline")

	for name, body := range map[string]map[string]any{
		"only input_reference may be uploaded": {"kind": "multipart", "fields": map[string][]string{"model": {model}, "prompt": {"a cat"}},
			"files": []any{map[string]any{"ref": "request_file:mask", "field": "mask", "filename": "m.png", "mimeType": "image/png", "size": 10}}},
		"input_reference must be provided once": {"kind": "multipart", "fields": map[string][]string{"model": {model}, "prompt": {"a cat"}, "input_reference": {"https://cdn.example/a.png"}},
			"files": []any{map[string]any{"ref": "request_file:input_reference", "field": "input_reference", "filename": "a.png", "mimeType": "image/png", "size": 10}}},
		"metadata must be a JSON object string": {"kind": "multipart", "fields": map[string][]string{"model": {model}, "prompt": {"a cat"}, "metadata": {"[]"}}},
	} {
		_, err := decode(body)
		require.ErrorContains(t, err, name, name)
	}
}
