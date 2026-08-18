// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	encasn1 "encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/cryptobyte"
	cbasn1 "golang.org/x/crypto/cryptobyte/asn1"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// --- Data Forwarding ---

func newProtectDataForwardingCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data-forwarding",
		Short: "Manage data forwarding settings",
	}

	cmd.AddCommand(newProtectDataForwardingGetCmd(cliCtx))
	cmd.AddCommand(newProtectDataForwardingUpdateCmd(cliCtx))

	return cmd
}

func newProtectDataForwardingGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Get data forwarding settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.GetDataForwarding(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectDataForwardingUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update data forwarding settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.DataForwardingInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			item, err := cliCtx.ProtectClient.UpdateDataForwarding(cmd.Context(), input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")

	return cmd
}

// --- Data Retention ---

func newProtectDataRetentionCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data-retention",
		Short: "Manage data retention settings",
	}

	cmd.AddCommand(newProtectDataRetentionGetCmd(cliCtx))
	cmd.AddCommand(newProtectDataRetentionUpdateCmd(cliCtx))

	return cmd
}

func newProtectDataRetentionGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Get data retention settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.GetDataRetention(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectDataRetentionUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update data retention settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.DataRetentionInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			item, err := cliCtx.ProtectClient.UpdateDataRetention(cmd.Context(), input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")

	return cmd
}

// --- Downloads ---

func newProtectDownloadsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "downloads",
		Short: "Get organization download links",
	}

	cmd.AddCommand(newProtectDownloadsSummaryCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsInstallerCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsUninstallerCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsPPPCCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsTamperPreventionCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsNetworkContentFilterCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsRootCACmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsCSRCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsWebsocketAuthCmd(cliCtx))

	return cmd
}

func newProtectDownloadsSummaryCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "summary",
		Short: "Show download metadata (version, URLs)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(ctx)
			if err != nil {
				return err
			}

			baseURL := cliCtx.ProtectClient.BaseURL()
			version := ""
			if downloads.VanillaPackage != nil {
				version = downloads.VanillaPackage.Version
			}

			summary := map[string]any{
				"installerVersion":               version,
				"installerURL":                   buildProtectPackageURL(baseURL, "installer.pkg", downloads.InstallerUUID),
				"uninstallerURL":                 buildProtectPackageURL(baseURL, "uninstaller.pkg", downloads.InstallerUUID),
				"hasPPPC":                        downloads.PPPC != "",
				"hasRootCA":                      downloads.RootCA != "",
				"hasCSR":                         downloads.CSR != "",
				"hasWebsocketAuth":               downloads.WebsocketAuth != "",
				"hasTamperPreventionProfile":     downloads.TamperPreventionProfile != "",
				"hasNetworkContentFilterProfile": downloads.NetworkContentFilterProfile != "",
			}
			data, err := json.Marshal(summary)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func newProtectDownloadsInstallerCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string

	cmd := &cobra.Command{
		Use:   "installer",
		Short: "Download the Jamf Protect installer package",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(ctx)
			if err != nil {
				return err
			}
			pkgURL := buildProtectPackageURL(cliCtx.ProtectClient.BaseURL(), "installer.pkg", downloads.InstallerUUID)
			if pkgURL == "" {
				return fmt.Errorf("installer package not available")
			}
			if outPath == "" {
				version := ""
				if downloads.VanillaPackage != nil {
					version = downloads.VanillaPackage.Version
				}
				outPath = fmt.Sprintf("JamfProtect-%s.pkg", version)
			}
			return downloadProtectFile(ctx, cliCtx.ProtectClient, pkgURL, outPath)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtect-{version}.pkg)")
	return cmd
}

func newProtectDownloadsUninstallerCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string

	cmd := &cobra.Command{
		Use:   "uninstaller",
		Short: "Download the Jamf Protect uninstaller package",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(ctx)
			if err != nil {
				return err
			}
			pkgURL := buildProtectPackageURL(cliCtx.ProtectClient.BaseURL(), "uninstaller.pkg", downloads.InstallerUUID)
			if pkgURL == "" {
				return fmt.Errorf("uninstaller package not available")
			}
			if outPath == "" {
				version := ""
				if downloads.VanillaPackage != nil {
					version = downloads.VanillaPackage.Version
				}
				outPath = fmt.Sprintf("JamfProtectUninstaller-%s.pkg", version)
			}
			return downloadProtectFile(ctx, cliCtx.ProtectClient, pkgURL, outPath)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtectUninstaller-{version}.pkg)")
	return cmd
}

func newProtectDownloadsPPPCCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string
	var unsigned bool

	cmd := &cobra.Command{
		Use:   "pppc-profile",
		Short: "Download the PPPC configuration profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(cmd.Context())
			if err != nil {
				return err
			}
			if downloads.PPPC == "" {
				return fmt.Errorf("PPPC profile not available")
			}
			if outPath == "" {
				outPath = "JamfProtect-PPPC.mobileconfig"
			}
			return writeProfileFile(downloads.PPPC, outPath, 0o644, unsigned)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtect-PPPC.mobileconfig)")
	cmd.Flags().BoolVar(&unsigned, "unsigned", false, "Strip the CMS signature, writing the raw payload (for re-uploading to Jamf Pro)")
	return cmd
}

func newProtectDownloadsTamperPreventionCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string
	var unsigned bool

	cmd := &cobra.Command{
		Use:   "tamper-prevention-profile",
		Short: "Download the tamper prevention system extension profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(cmd.Context())
			if err != nil {
				return err
			}
			if downloads.TamperPreventionProfile == "" {
				return fmt.Errorf("tamper prevention profile not available")
			}
			if outPath == "" {
				outPath = "JamfProtect-TamperPrevention.mobileconfig"
			}
			return writeProfileFile(downloads.TamperPreventionProfile, outPath, 0o644, unsigned)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtect-TamperPrevention.mobileconfig)")
	cmd.Flags().BoolVar(&unsigned, "unsigned", false, "Strip the CMS signature, writing the raw payload (for re-uploading to Jamf Pro)")
	return cmd
}

func newProtectDownloadsNetworkContentFilterCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string
	var unsigned bool

	cmd := &cobra.Command{
		Use:   "network-content-filter-profile",
		Short: "Download the network content filter configuration profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(cmd.Context())
			if err != nil {
				return err
			}
			if downloads.NetworkContentFilterProfile == "" {
				return fmt.Errorf("network content filter profile not available")
			}
			if outPath == "" {
				outPath = "JamfProtect-NetworkContentFilter.mobileconfig"
			}
			return writeProfileFile(downloads.NetworkContentFilterProfile, outPath, 0o644, unsigned)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtect-NetworkContentFilter.mobileconfig)")
	cmd.Flags().BoolVar(&unsigned, "unsigned", false, "Strip the CMS signature, writing the raw payload (for re-uploading to Jamf Pro)")
	return cmd
}

func newProtectDownloadsRootCACmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string

	cmd := &cobra.Command{
		Use:   "root-ca",
		Short: "Download the root CA certificate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(cmd.Context())
			if err != nil {
				return err
			}
			if downloads.RootCA == "" {
				return fmt.Errorf("root CA certificate not available")
			}
			if outPath == "" {
				outPath = "JamfProtect-RootCA.pem"
			}
			return writeBase64File(downloads.RootCA, outPath, 0o644)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtect-RootCA.pem)")
	return cmd
}

func newProtectDownloadsCSRCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string

	cmd := &cobra.Command{
		Use:   "csr",
		Short: "Download the CSR identity",
		RunE: func(cmd *cobra.Command, _ []string) error {
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(cmd.Context())
			if err != nil {
				return err
			}
			if downloads.CSR == "" {
				return fmt.Errorf("CSR identity not available")
			}
			if outPath == "" {
				outPath = "JamfProtect-CSR.p12"
			}
			return writeBase64File(downloads.CSR, outPath, 0o600)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtect-CSR.p12)")
	return cmd
}

func newProtectDownloadsWebsocketAuthCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string

	cmd := &cobra.Command{
		Use:   "websocket-auth",
		Short: "Download the websocket authorizer key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(cmd.Context())
			if err != nil {
				return err
			}
			if downloads.WebsocketAuth == "" {
				return fmt.Errorf("websocket authorizer key not available")
			}
			if outPath == "" {
				outPath = "JamfProtect-WebsocketAuth.p12"
			}
			return writeBase64File(downloads.WebsocketAuth, outPath, 0o600)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtect-WebsocketAuth.p12)")
	return cmd
}

// buildProtectPackageURL constructs a download URL for an installer/uninstaller package.
func buildProtectPackageURL(baseURL, packageName, installerUUID string) string {
	if baseURL == "" || installerUUID == "" {
		return ""
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	u = u.JoinPath(packageName)
	u.RawQuery = installerUUID
	return u.String()
}

// writeBase64File decodes a base64 string and writes the result to a file.
func writeBase64File(b64 string, path string, perm os.FileMode) error {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("decoding base64 data: %w", err)
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Saved to %s (%d bytes)\n", path, len(data))
	return nil
}

// writeProfileFile writes a base64-encoded configuration profile to disk. When
// unsigned is true, the PKCS7 (CMS) signature envelope is stripped and only the
// raw mobileconfig payload is written — required for re-uploading the profile to
// Jamf Pro, which signs profiles itself on delivery and rejects pre-signed input.
func writeProfileFile(b64 string, path string, perm os.FileMode, unsigned bool) error {
	if !unsigned {
		return writeBase64File(b64, path, perm)
	}

	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("decoding base64 data: %w", err)
	}
	payload, err := extractPKCS7Content(data)
	if err != nil {
		return fmt.Errorf("stripping signature: %w", err)
	}
	if err := os.WriteFile(path, payload, perm); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Saved to %s (%d bytes, signature stripped)\n", path, len(payload))
	return nil
}

// pkcs7SignedDataOID is the ASN.1 OID for the PKCS7 signedData content type.
var pkcs7SignedDataOID = encasn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}

// extractPKCS7Content unwraps a PKCS7 (CMS) SignedData envelope and returns the
// encapsulated content — the raw mobileconfig payload. It does NOT verify the
// signature; it only strips it so the profile can be re-uploaded to Jamf Pro,
// which signs profiles itself on delivery and rejects pre-signed input.
//
// Structure walked (RFC 5652):
//
//	ContentInfo ::= SEQUENCE { contentType OID, content [0] EXPLICIT SignedData }
//	SignedData  ::= SEQUENCE { version INTEGER, digestAlgorithms SET,
//	                           encapContentInfo SEQUENCE { eContentType OID,
//	                                                       eContent [0] EXPLICIT OCTET STRING } ... }
func extractPKCS7Content(der []byte) ([]byte, error) {
	input := cryptobyte.String(der)

	var contentInfo cryptobyte.String
	if !input.ReadASN1(&contentInfo, cbasn1.SEQUENCE) {
		return nil, fmt.Errorf("malformed ContentInfo (is the profile actually signed?)")
	}

	var contentType encasn1.ObjectIdentifier
	if !contentInfo.ReadASN1ObjectIdentifier(&contentType) {
		return nil, fmt.Errorf("malformed contentType")
	}
	if !contentType.Equal(pkcs7SignedDataOID) {
		return nil, fmt.Errorf("not a PKCS7 signedData envelope (contentType %v)", contentType)
	}

	explicitTag := cbasn1.Tag(0).Constructed().ContextSpecific()
	var signedDataWrapper cryptobyte.String
	if !contentInfo.ReadASN1(&signedDataWrapper, explicitTag) {
		return nil, fmt.Errorf("malformed SignedData wrapper")
	}

	var signedData cryptobyte.String
	if !signedDataWrapper.ReadASN1(&signedData, cbasn1.SEQUENCE) {
		return nil, fmt.Errorf("malformed SignedData")
	}
	if !signedData.SkipASN1(cbasn1.INTEGER) {
		return nil, fmt.Errorf("malformed SignedData version")
	}
	if !signedData.SkipASN1(cbasn1.SET) {
		return nil, fmt.Errorf("malformed digestAlgorithms")
	}

	var encapContentInfo cryptobyte.String
	if !signedData.ReadASN1(&encapContentInfo, cbasn1.SEQUENCE) {
		return nil, fmt.Errorf("malformed encapContentInfo")
	}
	if !encapContentInfo.SkipASN1(cbasn1.OBJECT_IDENTIFIER) {
		return nil, fmt.Errorf("malformed eContentType")
	}

	var eContentWrapper cryptobyte.String
	if !encapContentInfo.ReadASN1(&eContentWrapper, explicitTag) {
		return nil, fmt.Errorf("signed profile has no embedded payload")
	}
	var payload cryptobyte.String
	if !eContentWrapper.ReadASN1(&payload, cbasn1.OCTET_STRING) {
		return nil, fmt.Errorf("malformed eContent payload")
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("signed profile contained an empty payload")
	}
	return []byte(payload), nil
}

// downloadProtectFile downloads a file from a URL using the Protect client's auth token.
func downloadProtectFile(ctx context.Context, client registry.ProtectClient, fileURL, outPath string) error {
	token, err := client.AccessToken(ctx)
	if err != nil {
		return fmt.Errorf("getting access token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", token.TokenType+" "+token.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Saved to %s (%d bytes)\n", outPath, n)
	return nil
}

// --- Config Freeze ---

func newProtectConfigFreezeCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config-freeze",
		Short: "Manage configuration freeze",
	}

	cmd.AddCommand(newProtectConfigFreezeGetCmd(cliCtx))
	cmd.AddCommand(newProtectConfigFreezeEnableCmd(cliCtx))
	cmd.AddCommand(newProtectConfigFreezeDisableCmd(cliCtx))

	return cmd
}

func newProtectConfigFreezeGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Get config freeze status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.GetConfigFreeze(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectConfigFreezeEnableCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable config freeze",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.UpdateOrganizationConfigFreeze(cmd.Context(), true)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectConfigFreezeDisableCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable config freeze",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.UpdateOrganizationConfigFreeze(cmd.Context(), false)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

// --- Connections ---

func newProtectConnectionsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connections",
		Short: "Manage identity provider connections",
	}

	cmd.AddCommand(newProtectConnectionsListCmd(cliCtx))

	return cmd
}

func newProtectConnectionsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all connections",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListConnections(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintList(cliCtx.Output, items)
		},
	}
}

// dataRetentionToInput converts the retention settings response to the update
// input shape. The response is nested (database.log.numberOfDays) while the
// input is flat (DatabaseLogDays), so a backup that stored the response could
// not be replayed: decoding it into the input yielded zeros, and the server
// rejects 0 as "not one of [30, 60, 90, 180, 365]".
func dataRetentionToInput(s jamfprotect.DataRetentionSettings) jamfprotect.DataRetentionInput {
	var input jamfprotect.DataRetentionInput
	if s.Database != nil {
		if s.Database.Log != nil {
			input.DatabaseLogDays = s.Database.Log.NumberOfDays
		}
		if s.Database.Alert != nil {
			input.DatabaseAlertDays = s.Database.Alert.NumberOfDays
		}
	}
	if s.Cold != nil && s.Cold.Alert != nil {
		input.ColdAlertDays = s.Cold.Alert.NumberOfDays
	}
	return input
}

// redactDataForwarding removes the one third-party credential the forwarding
// settings response returns in cleartext.
//
// Legacy Sentinel declares sharedKey as a plain String and the SDK's query
// selects it; SentinelV2 got this right and reports secretExists instead. Since
// this resource is captured for reference and never replayed, dropping the value
// costs nothing and keeps it out of a backup directory that the documented
// workflow puts under version control.
func redactDataForwarding(r jamfprotect.DataForwardingResult) jamfprotect.DataForwardingResult {
	if r.Forward == nil || r.Forward.Sentinel == nil || r.Forward.Sentinel.SharedKey == "" {
		return r
	}
	sentinel := *r.Forward.Sentinel
	sentinel.SharedKey = protectRedacted
	forward := *r.Forward
	forward.Sentinel = &sentinel
	r.Forward = &forward
	return r
}
