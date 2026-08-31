// Copyright 2026, Jamf Software LLC

package privileges

// This file transcribes Jamf's "Jamf Pro permissions map" documentation
// article: the permission name and grouping Jamf Account shows for every GA
// capability. It is the only artefact that carries the mapping — the OpenAPI
// specs publish the capability slug and nothing else — so it is maintained by
// hand and guarded by TestCatalogueCoversEveryScopeThisCLISends, which fails
// when a spec starts requiring a capability this file has never heard of.
//
// It is a *rendering* table and nothing more. Which capability an operation
// requires is always read from a spec (specs/gateway/coverage.json for Pro and
// Classic, x-required-privileges for Platform); this file only says what Jamf
// Account calls it. That distinction is why adding a row here does not conflict
// with the rule in CLAUDE.md against hand-supplying the account APIs' missing
// privilege names: a row with no spec pointing at it renders nothing.
//
// Source: the permissionsMapURL below. Transcribed 2026-08-31, from the same
// revision terraform-provider-jamfplatform's internal/common/permissions
// transcribes — the two files are copies on purpose, since neither repo can
// import the other and Jamf publishes the mapping as prose. Re-verify against
// the article rather than against the other copy.
//
// No test asserts what an entry SAYS. TestCatalogueCoversEveryScopeThisCLISends
// checks only that a required capability HAS a row, so a wrong section or a
// wrong permission name is invisible to it. Re-verify by reading the article.
//
// The transcription is deliberately complete rather than trimmed to what this
// CLI calls: an entry costs one line, and keeping the file a faithful copy of
// the article makes it diffable against the next revision of it.
// permissionsMapURL is Jamf's "Jamf Pro permissions map" article: the page this
// file transcribes, and the page a rendered hint sends a reader to. Jamf
// Account's picker is searched by permission name, not by API capability slug,
// and the two differ substantially — computer-inventory-collection-settings is
// "Device inventory collection settings" — so the article is the only way to get
// from a slug to the box to tick.
const permissionsMapURL = "https://developer.jamf.com/platform-api/reference/jamf-pro-permissions-map"

// Category names in sentence case, as terraform-provider-jamfplatform's copy
// spells them. The article's own headings are Title Case ("Organizational
// Context", "Global Settings"), and which of the two Jamf Account's picker uses
// is unverified from here — the difference is cosmetic for finding a section, so
// the two transcriptions are kept identical rather than one of them guessing.
// Verified against the article's v1882-ai-governance revision on 2026-08-31:
// every permission name below is verbatim, all 125 rows.
//
// Declared in the order the article groups them so this file
// stays diffable against it. Nothing is derived from that order: a rendered
// hint sorts its rows by category name and then permission name, because
// Jamf Account's row order is a weaker contract than its names — the picker can
// be reordered without anything being renamed, and no test here could tell.
const (
	catOrganizationManagement = "Organization management"
	catInventory              = "Inventory"
	catOrganizationalContext  = "Organizational context"
	catDeviceActions          = "Device actions"
	catDeviceSecrets          = "Device secrets"
	catDeployment             = "Deployment"
	catEnrollment             = "Enrollment"
	catAppLifecycle           = "App lifecycle management"
	catCompliance             = "Compliance"
	catEndpointSecurity       = "Endpoint security"
	catSecureEnterpriseAccess = "Secure enterprise access"
	catAdminIdentity          = "Admin identity and access"
	catAdminFileUploads       = "Admin file uploads"
	catGlobalSettings         = "Global settings"
	catInfrastructure         = "Infrastructure"
)

// entry is one row of Jamf Account's permission picker: the section it sits
// under and the name printed beside its action checkboxes.
type entry struct {
	category string
	name     string
}

// catalogue maps a GA capability slug to the permission Jamf Account shows for
// it. Grouped and ordered as the article is.
var catalogue = map[string]entry{
	// Organization management scope.
	"licensing":           {catOrganizationManagement, "Licensing"},
	"deal-registration":   {catOrganizationManagement, "Partner deal registration"},
	"distributor-actions": {catOrganizationManagement, "Distributor actions"},
	"sso-connections":     {catOrganizationManagement, "SSO connections"},
	"sso-domains":         {catOrganizationManagement, "SSO domains"},

	// Inventory.
	"devices":                   {catInventory, "Devices"},
	"device-groups":             {catInventory, "Device groups"},
	"users":                     {catInventory, "Users"},
	"user-groups":               {catInventory, "User groups"},
	"extension-attributes":      {catInventory, "Device extension attributes"},
	"user-extension-attributes": {catInventory, "User extension attributes"},
	"advanced-device-searches":  {catInventory, "Advanced device searches"},
	"advanced-user-searches":    {catInventory, "Advanced user searches"},
	"device-history":            {catInventory, "Device history"},

	// Organizational context.
	"sites":            {catOrganizationalContext, "Sites"},
	"buildings":        {catOrganizationalContext, "Buildings"},
	"departments":      {catOrganizationalContext, "Departments"},
	"categories":       {catOrganizationalContext, "Categories"},
	"classes":          {catOrganizationalContext, "Classes"},
	"network-segments": {catOrganizationalContext, "Network segments"},
	"ibeacon":          {catOrganizationalContext, "iBeacon regions"},

	// Device actions.
	"device-actions":             {catDeviceActions, "Device actions"},
	"destructive-device-actions": {catDeviceActions, "Destructive device actions"},

	// Device secrets.
	"disk-encryption-recovery-key": {catDeviceSecrets, "FileVault recovery key"},
	"recovery-lock":                {catDeviceSecrets, "Recovery lock password"},
	"computer-device-lock-pin":     {catDeviceSecrets, "Device lock PIN"},
	"local-admin-passwords":        {catDeviceSecrets, "Local Admin Passwords (LAPS)"},

	// Deployment.
	"blueprints":                     {catDeployment, "Blueprints"},
	"declarations":                   {catDeployment, "Declarations reporting"},
	"configuration-profiles":         {catDeployment, "Configuration profiles"},
	"policies":                       {catDeployment, "Policies"},
	"scripts":                        {catDeployment, "Scripts"},
	"packages":                       {catDeployment, "Packages"},
	"printers":                       {catDeployment, "Printers"},
	"dock-items":                     {catDeployment, "Dock items"},
	"managed-software-updates":       {catDeployment, "Software updates"},
	"disk-encryption-configurations": {catDeployment, "Disk encryption"},
	"directory-bindings":             {catDeployment, "Directory bindings"},
	"jamf-connect-deployments":       {catDeployment, "Jamf Connect deployment"},
	"jamf-protect-deployments":       {catDeployment, "Jamf Protect deployment"},

	// Enrollment.
	"prestage-enrollments":   {catEnrollment, "PreStage enrollments"},
	"enrollment-profiles":    {catEnrollment, "Enrollment profiles"},
	"enrollment-invitations": {catEnrollment, "Enrollment invitations"},
	"activation-profiles":    {catEnrollment, "Activation profiles"},

	// App lifecycle management.
	"applications":                     {catAppLifecycle, "Apps"},
	"jamf-packages-action":             {catAppLifecycle, "App package information"},
	"volume-purchasing-locations":      {catAppLifecycle, "Volume purchasing"},
	"ebooks":                           {catAppLifecycle, "eBooks"},
	"provisioning-profiles":            {catAppLifecycle, "Provisioning profiles for in-house apps"},
	"licensed-software":                {catAppLifecycle, "Licensed software"},
	"restricted-software":              {catAppLifecycle, "Restricted software"},
	"patch-policies":                   {catAppLifecycle, "Patch policies"},
	"patch-management-software-titles": {catAppLifecycle, "Patch titles"},
	"patch-external-source":            {catAppLifecycle, "External patch sources"},
	"patch-internal-source":            {catAppLifecycle, "Internal patch sources"},

	// Compliance.
	"ai-policies":                   {catCompliance, "AI policies"},
	"compliance-benchmarks":         {catCompliance, "Compliance Benchmarks"},
	"device-compliance-information": {catCompliance, "Conditional access device compliance"},

	// Endpoint security — reached through the Jamf Protect API, so these map to
	// GraphQL operation names rather than paths.
	"protection-plans":           {catEndpointSecurity, "Protection plans"},
	"detection-analytics":        {catEndpointSecurity, "Detection analytics"},
	"threat-alerts":              {catEndpointSecurity, "Threat alerts"},
	"prevent-lists":              {catEndpointSecurity, "Prevent lists"},
	"threat-definition-versions": {catEndpointSecurity, "Threat definition versions"},
	"unified-logging-filters":    {catEndpointSecurity, "Unified logging filters"},
	"security-audit-log":         {catEndpointSecurity, "Security audit log"},

	// Secure enterprise access.
	"ztna":                     {catSecureEnterpriseAccess, "Zero-Trust Network Access (ZTNA)"},
	"search-domains":           {catSecureEnterpriseAccess, "Search domains"},
	"custom-hostname-mappings": {catSecureEnterpriseAccess, "Custom hostname mappings"},
	"content-categories":       {catSecureEnterpriseAccess, "Content categories"},

	// Admin identity and access.
	"audit":             {catAdminIdentity, "Audit events"},
	"accounts":          {catAdminIdentity, "Admin account"},
	"change-password":   {catAdminIdentity, "Change admin password"},
	"account-groups":    {catAdminIdentity, "Admin account groups"},
	"ldap-servers":      {catAdminIdentity, "LDAP / cloud IdP"},
	"sso-settings":      {catAdminIdentity, "Single Sign-On"},
	"access-management": {catAdminIdentity, "Access management"},
	"user-sessions":     {catAdminIdentity, "Admin user sessions"},

	// Admin file uploads — one endpoint attaches files to many object types
	// under this single capability.
	"file-uploads": {catAdminFileUploads, "Admin file uploads"},

	// Global settings.
	"uem-connect":                            {catGlobalSettings, "UEM Connect configuration"},
	"conditional-access":                     {catGlobalSettings, "Microsoft Intune conditional access configuration"},
	"self-service":                           {catGlobalSettings, "Self Service configuration"},
	"app-request":                            {catGlobalSettings, "App request settings"},
	"onboarding":                             {catGlobalSettings, "Onboarding configuration"},
	"re-enrollment":                          {catGlobalSettings, "Re-enrollment settings"},
	"return-to-service":                      {catGlobalSettings, "Return to service configuration"},
	"user-initiated-enrollment":              {catGlobalSettings, "User-initiated enrollment settings"},
	"apple-configurator-enrollment":          {catGlobalSettings, "Apple Configurator enrollment settings"},
	"enrollment-customization":               {catGlobalSettings, "Enrollment customization"},
	"teacher-app":                            {catGlobalSettings, "Teacher app settings"},
	"parent-app":                             {catGlobalSettings, "Parent app settings"},
	"remote-assist":                          {catGlobalSettings, "Remote Assist"},
	"remote-administration":                  {catGlobalSettings, "TeamViewer configuration"},
	"computer-check-in":                      {catGlobalSettings, "Device check-in configuration"},
	"computer-inventory-collection-settings": {catGlobalSettings, "Device inventory collection settings"},
	"custom-paths":                           {catGlobalSettings, "Device inventory collection custom file paths"},
	"removable-mac-address":                  {catGlobalSettings, "Removable MAC addresses"},
	"inventory-preload-records":              {catGlobalSettings, "Inventory preload"},
	"mdm-profile-renewal-settings":           {catGlobalSettings, "MDM profile renewal settings"},
	"impact-alert-notification-settings":     {catGlobalSettings, "Notification settings"},
	"dismiss-notifications":                  {catGlobalSettings, "Dismiss notifications"},
	"login-disclaimer":                       {catGlobalSettings, "Login disclaimer"},
	"webhooks":                               {catGlobalSettings, "Webhooks"},
	"allowed-file-extension":                 {catGlobalSettings, "Allowed file upload extensions"},

	// Infrastructure.
	"device-enrollment-program-instances":   {catInfrastructure, "Automated Device Enrollment connection"},
	"pki":                                   {catInfrastructure, "PKI certificates"},
	"ad-cs-settings":                        {catInfrastructure, "Active Directory Certificate Services connector"},
	"digicert-settings":                     {catInfrastructure, "DigiCert Trust Lifecycle Manager"},
	"push-certificates":                     {catInfrastructure, "APNS certificate"},
	"gsx-connection":                        {catInfrastructure, "Apple GSX connection"},
	"distribution-points":                   {catInfrastructure, "Distribution points"},
	"cloud-distribution-point":              {catInfrastructure, "Cloud Distribution Point"},
	"jamf-cloud-distribution-service-files": {catInfrastructure, "Jamf Cloud Distribution Service files"},
	"json-web-token-configuration":          {catInfrastructure, "JSON web token configuration"},
	"software-update-servers":               {catInfrastructure, "Software update servers"},
	"smtp-server":                           {catInfrastructure, "SMTP"},
	"cache":                                 {catInfrastructure, "Cache"},
	"cloud-services-settings":               {catInfrastructure, "Jamf Cloud Services connection"},
	"apache-tomcat-settings":                {catInfrastructure, "Tomcat server"},
	"infrastructure-managers":               {catInfrastructure, "Infrastructure Manager instances"},
	"retention-policy":                      {catInfrastructure, "Retention policy"},
	"flush-policy-logs":                     {catInfrastructure, "Log flushing"},
	"activation-code":                       {catInfrastructure, "Activation code"},
	"jss-information":                       {catInfrastructure, "Jamf Pro SLASA"},
	"m2m":                                   {catInfrastructure, "M2M tenant ID"},
	"jss-url":                               {catInfrastructure, "Jamf Pro server URL"},
}

// actionOrder is the article's own ordering of the six actions, and actionLabels
// the words Jamf Account prints beside each checkbox. Rendering an action set in
// this order rather than alphabetically keeps "Create, Read, Update, Delete"
// reading as the CRUD sequence an operator expects.
var actionOrder = []string{"create", "read", "update", "delete", "deploy", "execute"}

var actionLabels = map[string]string{
	"create":  "Create",
	"read":    "Read",
	"update":  "Update",
	"delete":  "Delete",
	"deploy":  "Deploy",
	"execute": "Execute",
}
