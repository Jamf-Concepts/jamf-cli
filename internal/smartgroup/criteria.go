// Copyright 2026, Jamf Software LLC

package smartgroup

// Smart-group criterion names. These strings must match Jamf Pro's smart-group
// criterion UI exactly. The canonical source is the JSS server repo
// (jamf/jss). Each const cites the file:line it was sourced from. Re-verify
// after any sync-specs pass that includes JSS source updates.

const (
	// FileVault2StatusMatcher.java:@Component("FileVault 2 Status")
	CriterionFV2Status = "FileVault 2 Status"

	// MatcherNameConstants.java:CD.FILE_VAULT_2_ENABLED
	CriterionFV2Enabled = "FileVault 2 Enabled"

	// ComputerInventoryValues.java:103
	CriterionFV2RecoveryKeyType = "FileVault 2 Recovery Key Type"

	// ComputerInventoryValues.java:104
	CriterionFV2IndividualKeyValidation = "FileVault 2 Individual Key Validation"

	// ComputerInventoryValues.java:106
	CriterionFV2PersonalRecoveryKey = "FileVault 2 Personal Recovery Key"

	// MatcherNameConstants.java:CD.OPERATING_SYSTEM_VERSION
	CriterionOSVersion = "Operating System Version"

	// MatcherNameConstants.java:CD.OPERATING_SYSTEM_BUILD
	CriterionOSBuild = "Operating System Build"

	// MatcherNameConstants.java:CD.OPERATING_SYSTEM_SUPPLEMENTAL_VERSION_EXTRA
	CriterionOSRapidSecurityResponse = "Operating System Rapid Security Response"

	// MatcherNameConstants.java:MDD.LAST_INVENTORY_UPDATE
	CriterionLastInventoryUpdate = "Last Inventory Update"

	// MatcherNameConstants.java:MDD.BOOTSTRAP_TOKEN_ESCROWED
	CriterionBootstrapTokenEscrowed = "Bootstrap Token Escrowed"

	// UserApprovedMdmMatcher.java:@Component("User Approved MDM")
	CriterionUserApprovedMDM = "User Approved MDM"

	// MatcherNameConstants.java:MDD.MDM_PROFILE_EXPIRATION_DATE
	CriterionMDMProfileExpirationDate = "MDM Profile Expiration Date"

	// MatcherNameConstants.java:CD.DECLARATIVE_DEVICE_MANAGEMENT_ENABLED
	CriterionDDMEnabled = "Declarative Device Management Enabled"

	// ComputerInventoryValues.java:118
	CriterionGatekeeper = "Gatekeeper"

	// ComputerInventoryValues.java:119
	CriterionSIP = "System Integrity Protection"

	// MatcherNameConstants.java:CD.FIREWALL_ENABLED
	CriterionFirewallEnabled = "Firewall Enabled"

	// MatcherNameConstants.java:MDD.SUPERVISED
	CriterionSupervised = "Supervised"

	// MatcherNameConstants.java:E.PRESTAGE
	CriterionEnrollmentMethodPrestage = "Enrollment Method: PreStage enrollment"

	// MatcherNameConstants.java:CD.APPLE_SILICON
	CriterionAppleSilicon = "Apple Silicon"

	// Parallel inventory criterion; pro smart-group verify-templates is the
	// empirical check against a live tenant.
	CriterionJamfBinaryVersion = "Jamf Binary Version"
)

// allCriterionConsts returns the full registry as a map for testing.
// Keep in sync with the const block above.
func allCriterionConsts() map[string]string {
	return map[string]string{
		"CriterionFV2Status":                  CriterionFV2Status,
		"CriterionFV2Enabled":                 CriterionFV2Enabled,
		"CriterionFV2RecoveryKeyType":         CriterionFV2RecoveryKeyType,
		"CriterionFV2IndividualKeyValidation": CriterionFV2IndividualKeyValidation,
		"CriterionFV2PersonalRecoveryKey":     CriterionFV2PersonalRecoveryKey,
		"CriterionOSVersion":                  CriterionOSVersion,
		"CriterionOSBuild":                    CriterionOSBuild,
		"CriterionOSRapidSecurityResponse":    CriterionOSRapidSecurityResponse,
		"CriterionLastInventoryUpdate":        CriterionLastInventoryUpdate,
		"CriterionBootstrapTokenEscrowed":     CriterionBootstrapTokenEscrowed,
		"CriterionUserApprovedMDM":            CriterionUserApprovedMDM,
		"CriterionMDMProfileExpirationDate":   CriterionMDMProfileExpirationDate,
		"CriterionDDMEnabled":                 CriterionDDMEnabled,
		"CriterionGatekeeper":                 CriterionGatekeeper,
		"CriterionSIP":                        CriterionSIP,
		"CriterionFirewallEnabled":            CriterionFirewallEnabled,
		"CriterionSupervised":                 CriterionSupervised,
		"CriterionEnrollmentMethodPrestage":   CriterionEnrollmentMethodPrestage,
		"CriterionAppleSilicon":               CriterionAppleSilicon,
		"CriterionJamfBinaryVersion":          CriterionJamfBinaryVersion,
	}
}
