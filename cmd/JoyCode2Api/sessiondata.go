package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCode2Api/pkg/sessionguard"
	"github.com/vibe-coding-labs/JoyCode2Api/pkg/store"
)

// sessionDataCmd manages the local session cache used for session integrity
// checks. Hidden from help: invoked as
//
//	JoyCode2Api session-data            # print this machine's diag code
//	JoyCode2Api session-data <token>    # import an issued token
//
// Store paths mirror the serve command so both see the same database.
var sessionDataCmd = &cobra.Command{
	Use:    "session-data [token]",
	Short:  "诊断或更新本地会话缓存",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open("")
		if err != nil {
			return fmt.Errorf("open store: %w", err)
		}
		defer s.Close()

		if len(args) == 0 {
			fmt.Println("诊断码:", sessionguard.DiagCode())
			v := sessionguard.NewVerifier(s.GetSetting, s.SetSetting)
			if d, ok := v.Status(); ok {
				fmt.Printf("会话状态: 有效,剩余 %s\n", d.Round(0).String())
			} else {
				fmt.Println("会话状态: 未激活")
			}
			return nil
		}

		v := sessionguard.NewVerifier(s.GetSetting, s.SetSetting)
		if err := v.Import(strings.Join(args, "")); err != nil {
			return err
		}
		d, _ := v.Status()
		fmt.Printf("会话已更新,剩余有效期 %s\n", d.Round(0).String())
		return nil
	},
}

// sessionDiagCmd prints machine diagnostics; with --self-issue it emits a
// short-lived token signed with the dev fallback key. Self-issue works only
// on dev builds (production binaries embed a real public key, so a dev-key
// signature fails verification and self-issue reports that instead).
var sessionDiagCmd = &cobra.Command{
	Use:    "session-diag",
	Short:  "本机会话诊断",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if selfIssue {
			tok, err := sessionguard.SelfIssueDev(24 * time.Hour)
			if err != nil {
				return err
			}
			fmt.Println(tok)
			return nil
		}
		fmt.Println("诊断码:", sessionguard.DiagCode())
		return nil
	},
}

var selfIssue bool

func init() {
	sessionDiagCmd.Flags().BoolVar(&selfIssue, "self-issue", false, "issue a short-lived dev token for this machine")
	if os.Getenv("JOYCODE_SESSION_MGMT") == "1" {
		rootCmd.AddCommand(sessionDataCmd)
	}
	// session-diag is registered unconditionally but hidden: operators need it
	// for diag codes, and its self-issue mode is inert on production keys.
	rootCmd.AddCommand(sessionDiagCmd)
}
