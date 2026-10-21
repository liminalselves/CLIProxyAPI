package handlers

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

func TestChannelRoutingWithSlashModel(t *testing.T) {
	handler, cleanup := newChannelModeHandler(t)
	defer cleanup()
	const model = "vanchin/deepseek-v4.1-flash"
	const pinned = "ch1-kimi#1/" + model
	registry.GetGlobalRegistry().RegisterClient("bare-routing-auth", "kimi", []*registry.ModelInfo{{ID: pinned}})
	for _, tc := range []struct {
		model string
		bare  bool
		code  string
	}{
		{model: model, code: "channel_required"},
		{model: model, bare: true},
		{model: pinned},
		{model: pinned, bare: true},
		{model: "vanchin/missing", bare: true, code: "model_not_found"},
	} {
		t.Run(tc.model+"/"+tc.code, func(t *testing.T) {
			providers, normalized, errMsg := handler.getRequestDetailsWithOptions(tc.model, false, tc.bare)
			if tc.code != "" {
				if errMsg == nil || !strings.Contains(errMsg.Error.Error(), tc.code) {
					t.Fatalf("error = %#v, want %s", errMsg, tc.code)
				}
				return
			}
			if errMsg != nil || len(providers) != 1 || providers[0] != "kimi" || normalized != tc.model {
				t.Fatalf("route = %v %q %v", providers, normalized, errMsg)
			}
		})
	}
}
