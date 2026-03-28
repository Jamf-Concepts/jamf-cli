package commands

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func newProtectPlansCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plans",
		Short: "Manage Jamf Protect plans",
	}

	cmd.AddCommand(newProtectPlansListCmd(cliCtx))
	cmd.AddCommand(newProtectPlansGetCmd(cliCtx))
	cmd.AddCommand(newProtectPlansApplyCmd(cliCtx))
	cmd.AddCommand(newProtectPlansDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectPlansConfigProfileCmd(cliCtx))

	return cmd
}

func newProtectPlansListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all plans",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plans, err := cliCtx.ProtectClient.ListPlans(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(plans))
			for _, p := range plans {
				rows = append(rows, flattenPlan(p))
			}
			data, _ := json.Marshal(rows)
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenPlan converts a Plan into a clean map for readable table output,
// reducing nested objects to names/counts.
func flattenPlan(p jamfprotect.Plan) map[string]any {
	m := map[string]any{
		"name":        p.Name,
		"description": p.Description,
		"logLevel":    p.LogLevel,
		"autoUpdate":  p.AutoUpdate,
	}

	if p.ActionConfigs != nil {
		m["actionConfig"] = p.ActionConfigs.Name
	}

	if p.TelemetryV2 != nil {
		m["telemetry"] = p.TelemetryV2.Name
	} else if p.Telemetry != nil {
		m["telemetry"] = p.Telemetry.Name
	}

	if p.USBControlSet != nil {
		m["usbControlSet"] = p.USBControlSet.Name
	}

	return m
}

func newProtectPlansGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a plan by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolvePlanID(ctx, args[0])
			if err != nil {
				return err
			}

			plan, err := cliCtx.ProtectClient.GetPlan(ctx, id)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, plan)
		},
	}
}

func newProtectPlansApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		scaffold bool
		yes      bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a plan",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				fmt.Println(planScaffoldJSON)
				return nil
			}

			ctx := cmd.Context()
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}

			var input jamfprotect.PlanInput
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if plan exists by name
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolvePlanID(ctx, input.Name)

			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreatePlan(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created plan %q\n", input.Name)
				return protect.PrintOne(cliCtx.Output, result)
			}

			// Found — confirm before replacing
			proceed, err := confirmProtectReplace("plan", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := cliCtx.ProtectClient.UpdatePlan(ctx, id, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated plan %q\n", input.Name)
			return protect.PrintOne(cliCtx.Output, result)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print a JSON template for the request body and exit")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.MarkFlagsMutuallyExclusive("from-file", "scaffold")

	return cmd
}

func newProtectPlansDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolvePlanID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmProtectDelete("plan", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeletePlan(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted plan %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newProtectPlansConfigProfileCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		outPath string
		sign    bool
		noPPPC              bool
		noToken             bool
		noCA                bool
		noCSR               bool
		noWebsocket         bool
		noSystemExtension   bool
		noServiceManagement bool
		noXPC               bool
		noKeychainClientID  bool
	)

	cmd := &cobra.Command{
		Use:   "config-profile <name>",
		Short: "Download the configuration profile for a plan",
		Long: `Download the configuration profile (.mobileconfig) for a Jamf Protect plan.

By default, all payload components are included. Use --no-* flags to
exclude specific payloads. Use --sign to cryptographically sign the profile.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolvePlanID(ctx, args[0])
			if err != nil {
				return err
			}

			opts := &jamfprotect.PlanConfigProfileOptionsInput{
				Sign:              sign,
				PPPC:              !noPPPC,
				Token:             !noToken,
				CA:                !noCA,
				CSR:               !noCSR,
				Websocket:         !noWebsocket,
				SystemExtension:   !noSystemExtension,
				ServiceManagement: !noServiceManagement,
				TokenOptions: jamfprotect.PlanConfigProfileTokenOptionsInput{
					XPC:              !noXPC,
					KeychainClientID: !noKeychainClientID,
				},
			}

			profile, err := cliCtx.ProtectClient.GetPlansConfigProfile(ctx, id, opts)
			if err != nil {
				return err
			}

			if profile == "" {
				return fmt.Errorf("no configuration profile available for plan %q", args[0])
			}

			decoded, err := base64.StdEncoding.DecodeString(profile)
			if err != nil {
				return fmt.Errorf("decoding profile: %w", err)
			}

			if outPath == "" {
				outPath = fmt.Sprintf("%s.mobileconfig", args[0])
			}
			if err := os.WriteFile(outPath, decoded, 0o644); err != nil {
				return fmt.Errorf("writing profile: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Saved to %s (%d bytes)\n", outPath, len(decoded))
			return nil
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: <plan-name>.mobileconfig)")
	cmd.Flags().BoolVar(&sign, "sign", false, "Cryptographically sign the profile")
	cmd.Flags().BoolVar(&noPPPC, "no-pppc", false, "Exclude PPPC (Privacy Preferences) payload")
	cmd.Flags().BoolVar(&noToken, "no-token", false, "Exclude bootstrap token payload")
	cmd.Flags().BoolVar(&noCA, "no-ca", false, "Exclude root CA certificate payload")
	cmd.Flags().BoolVar(&noCSR, "no-csr", false, "Exclude CSR certificate payload")
	cmd.Flags().BoolVar(&noWebsocket, "no-websocket", false, "Exclude websocket authorizer key payload")
	cmd.Flags().BoolVar(&noSystemExtension, "no-system-extension", false, "Exclude system extension payload")
	cmd.Flags().BoolVar(&noServiceManagement, "no-service-management", false, "Exclude service management (login items) payload")
	cmd.Flags().BoolVar(&noXPC, "no-xpc", false, "Exclude XPC configuration from token")
	cmd.Flags().BoolVar(&noKeychainClientID, "no-keychain-client-id", false, "Exclude keychain client ID from token")

	return cmd
}

const planScaffoldJSON = `{
  "Name": "",
  "Description": "",
  "LogLevel": null,
  "ActionConfigs": "",
  "ExceptionSets": [],
  "Telemetry": null,
  "TelemetryV2": null,
  "AnalyticSets": [
    {
      "Type": "",
      "UUID": ""
    }
  ],
  "USBControlSet": null,
  "CommsConfig": {
    "FQDN": "",
    "Protocol": ""
  },
  "InfoSync": {
    "Attrs": [],
    "InsightsSyncInterval": 0
  },
  "AutoUpdate": false,
  "SignaturesFeedConfig": {
    "Mode": ""
  }
}`
