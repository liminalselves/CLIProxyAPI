package cmd

import (
	"context"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	log "github.com/sirupsen/logrus"
)

func DoZCodeLogin(cfg *config.Config, options *LoginOptions) {
	if options == nil {
		options = &LoginOptions{}
	}
	record, savedPath, errLogin := newAuthManager().Login(context.Background(), "zcode", cfg, &sdkAuth.LoginOptions{
		NoBrowser: options.NoBrowser, Prompt: options.Prompt,
	})
	if errLogin != nil {
		log.Errorf("ZCode authentication failed: %v", errLogin)
		return
	}
	if savedPath != "" {
		fmt.Printf("Authentication saved to %s\n", savedPath)
	}
	if record != nil && record.Label != "" {
		fmt.Printf("Authenticated as %s\n", record.Label)
	}
	fmt.Println("ZCode authentication successful!")
}
