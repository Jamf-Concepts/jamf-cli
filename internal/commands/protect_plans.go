package commands

import (
	"context"
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
	cmd.AddCommand(newProtectPlansExportCmd(cliCtx))

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
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenPlan converts a Plan into a clean map for readable table output,
// reducing nested objects to names/counts.
func flattenPlan(p jamfprotect.Plan) map[string]any {
	m := map[string]any{
		"name":       p.Name,
		"logLevel":   p.LogLevel,
		"autoUpdate": p.AutoUpdate,
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
			return printProtectResult(cliCtx.Output, plan, flattenPlan(*plan))
		},
	}
}

func newProtectPlansApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a plan",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}

			var export planExport
			if err := unmarshalProtectInput(data, &export); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}

			if export.Name == "" {
				return fmt.Errorf("input must include a 'name' field")
			}

			r := protect.NewResolver(cliCtx.ProtectClient)

			// Resolve names to IDs
			input, err := planExportToInput(ctx, export, r)
			if err != nil {
				return err
			}

			// Check if plan exists by name
			id, err := r.ResolvePlanID(ctx, input.Name)
			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreatePlan(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created plan %q\n", input.Name)
				return printProtectResult(cliCtx.Output, result, flattenPlan(result))
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
			return printProtectResult(cliCtx.Output, result, flattenPlan(result))
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")

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
		outPath             string
		sign                bool
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

func newProtectPlansExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a plan as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolvePlanID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetPlan(ctx, id)
			if err != nil {
				return err
			}
			return printProtectExport(planToExport(item))
		},
	}
}

// planExport is the human-friendly export/import format for plans.
// References use names instead of IDs/UUIDs so files are portable across tenants.
type planExport struct {
	Name                 string                                     `json:"name" yaml:"name"`
	Description          string                                     `json:"description" yaml:"description"`
	LogLevel             string                                     `json:"logLevel,omitempty" yaml:"logLevel,omitempty"`
	AutoUpdate           bool                                       `json:"autoUpdate" yaml:"autoUpdate"`
	ActionConfig         string                                     `json:"actionConfig" yaml:"actionConfig"`
	ExceptionSets        []string                                   `json:"exceptionSets,omitempty" yaml:"exceptionSets,omitempty"`
	AnalyticSets         []planAnalyticSetExport                    `json:"analyticSets,omitempty" yaml:"analyticSets,omitempty"`
	USBControlSet        string                                     `json:"usbControlSet,omitempty" yaml:"usbControlSet,omitempty"`
	Telemetry            string                                     `json:"telemetry,omitempty" yaml:"telemetry,omitempty"`
	CommsConfig          *jamfprotect.PlanCommsConfigInput          `json:"commsConfig,omitempty" yaml:"commsConfig,omitempty"`
	InfoSync             *jamfprotect.PlanInfoSyncInput             `json:"infoSync,omitempty" yaml:"infoSync,omitempty"`
	SignaturesFeedConfig *jamfprotect.PlanSignaturesFeedConfigInput `json:"signaturesFeedConfig,omitempty" yaml:"signaturesFeedConfig,omitempty"`
}

type planAnalyticSetExport struct {
	Name string `json:"name" yaml:"name"`
	Type string `json:"type" yaml:"type"`
}

// planToExport converts a Plan API response to the human-friendly export format.
func planToExport(p *jamfprotect.Plan) planExport {
	e := planExport{
		Name:        p.Name,
		Description: p.Description,
		LogLevel:    p.LogLevel,
		AutoUpdate:  p.AutoUpdate,
	}
	if p.ActionConfigs != nil {
		e.ActionConfig = p.ActionConfigs.Name
	}
	if len(p.ExceptionSets) > 0 {
		names := make([]string, len(p.ExceptionSets))
		for i, es := range p.ExceptionSets {
			names[i] = es.Name
		}
		e.ExceptionSets = names
	}
	if len(p.AnalyticSets) > 0 {
		sets := make([]planAnalyticSetExport, len(p.AnalyticSets))
		for i, as := range p.AnalyticSets {
			sets[i] = planAnalyticSetExport{
				Name: as.AnalyticSet.Name,
				Type: as.Type,
			}
		}
		e.AnalyticSets = sets
	}
	if p.USBControlSet != nil {
		e.USBControlSet = p.USBControlSet.Name
	}
	if p.TelemetryV2 != nil {
		e.Telemetry = p.TelemetryV2.Name
	} else if p.Telemetry != nil {
		e.Telemetry = p.Telemetry.Name
	}
	if p.CommsConfig != nil {
		e.CommsConfig = &jamfprotect.PlanCommsConfigInput{
			FQDN:     p.CommsConfig.FQDN,
			Protocol: p.CommsConfig.Protocol,
		}
	}
	if p.InfoSync != nil {
		e.InfoSync = &jamfprotect.PlanInfoSyncInput{
			Attrs:                p.InfoSync.Attrs,
			InsightsSyncInterval: p.InfoSync.InsightsSyncInterval,
		}
	}
	if p.SignaturesFeedConfig != nil {
		e.SignaturesFeedConfig = &jamfprotect.PlanSignaturesFeedConfigInput{
			Mode: p.SignaturesFeedConfig.Mode,
		}
	}
	return e
}

// planExportToInput resolves names to IDs and builds a PlanInput for the SDK.
func planExportToInput(ctx context.Context, e planExport, r *protect.Resolver) (jamfprotect.PlanInput, error) {
	input := jamfprotect.PlanInput{
		Name:        e.Name,
		Description: e.Description,
		AutoUpdate:  e.AutoUpdate,
	}
	if e.LogLevel != "" {
		input.LogLevel = &e.LogLevel
	}
	if e.ActionConfig != "" {
		id, err := r.ResolveActionConfigID(ctx, e.ActionConfig)
		if err != nil {
			return input, fmt.Errorf("resolving action config %q: %w", e.ActionConfig, err)
		}
		input.ActionConfigs = id
	}
	if len(e.ExceptionSets) > 0 {
		uuids := make([]string, len(e.ExceptionSets))
		for i, name := range e.ExceptionSets {
			uuid, err := r.ResolveExceptionSetUUID(ctx, name)
			if err != nil {
				return input, fmt.Errorf("resolving exception set %q: %w", name, err)
			}
			uuids[i] = uuid
		}
		input.ExceptionSets = uuids
	}
	if len(e.AnalyticSets) > 0 {
		sets := make([]jamfprotect.PlanAnalyticSetInput, len(e.AnalyticSets))
		for i, as := range e.AnalyticSets {
			uuid, err := r.ResolveAnalyticSetUUID(ctx, as.Name)
			if err != nil {
				return input, fmt.Errorf("resolving analytic set %q: %w", as.Name, err)
			}
			sets[i] = jamfprotect.PlanAnalyticSetInput{Type: as.Type, UUID: uuid}
		}
		input.AnalyticSets = sets
	}
	if e.USBControlSet != "" {
		id, err := r.ResolveRemovableStorageControlSetID(ctx, e.USBControlSet)
		if err != nil {
			return input, fmt.Errorf("resolving USB control set %q: %w", e.USBControlSet, err)
		}
		input.USBControlSet = &id
	}
	if e.Telemetry != "" {
		id, err := r.ResolveTelemetryV2ID(ctx, e.Telemetry)
		if err != nil {
			return input, fmt.Errorf("resolving telemetry %q: %w", e.Telemetry, err)
		}
		input.TelemetryV2 = &id
	}
	if e.CommsConfig != nil {
		input.CommsConfig = *e.CommsConfig
	}
	if e.InfoSync != nil {
		input.InfoSync = *e.InfoSync
	}
	if e.SignaturesFeedConfig != nil {
		input.SignaturesFeedConfig = *e.SignaturesFeedConfig
	}
	return input, nil
}
