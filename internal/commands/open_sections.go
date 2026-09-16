// Copyright 2026, Jamf Software LLC

package commands

// proUISection is a page of the Jamf Pro web interface `pro open` can name.
// Label is the heading the interface itself uses, which is what makes the
// section list and shell completion searchable by what someone saw on screen:
// several pages share a label under different parents ("Extension attributes"
// appears three times), so the name disambiguates and the label recognises.
type proUISection struct {
	Path  string
	Label string
}

// proUISections maps a section name to its path under the Jamf Pro base URL.
//
// Derived from a Jamf Pro 11.31 web session, from the three places the
// interface declares its own routes: the Angular router's legacy-page table
// (the `*.html` entries), the left-hand navigation (`{text, href}`), and the
// Settings cards (`cardTitle` / `cardPagePath`). The two curated sources are
// what bound the list — a page the interface links to is a page someone can be
// sent to — with a handful of collection pages added that no nav entry
// reaches, and the record-detail and wizard pages left out: those need state
// or an id, so opening one bare lands on an error rather than a page. Those
// stay reachable, because an argument that looks like a path is passed
// through.
//
// A route table is the only honest source for this. An unauthenticated request
// answers 302 to the login page for every path, real or invented, so the
// server cannot be asked whether a page exists, and the interface is a
// single-page app whose routes never appear as requests in a session
// recording.
//
// Names follow this CLI's own resource names wherever the interface's page is
// the same object (`policies`, `smart-computer-groups`, `packages`), so a
// reader who knows `pro policies list` can guess `pro open policies`. Settings
// pages are prefixed `settings/` and drop the interface's own category
// segment: the category is how the Settings page is organised on screen, not
// something anyone would type, and dropping it leaves no collisions.
//
// Paths are relative and carry no leading slash. They are joined onto the base
// URL by proSectionURL, which refuses anything that would leave the host.
var proUISections = map[string]proUISection{
	"account/change-password":                              {"view/account/change-password", "Change Password"},
	"account/preferences":                                  {"view/account/preferences", "Account Preferences"},
	"advanced-computer-searches":                           {"advancedComputerSearches.html", "Advanced computer searches"},
	"advanced-mobile-device-searches":                      {"advancedMobileDeviceSearches.html", "Advanced mobile device searches"},
	"advanced-user-content-searches":                       {"advancedUserContentSearches.html", "Advanced volume content searches"},
	"advanced-user-searches":                               {"advancedUserSearches.html", "Advanced user searches"},
	"app-installer-deployments":                            {"view/computers/mac-apps/app-installers/deployments", "App Installers deployments"},
	"blueprints":                                           {"view/mfe/blueprints", "Blueprints"},
	"classes":                                              {"classes.html", "Classes"},
	"compliance-benchmarks":                                {"view/mfe/compliance-benchmarks", "Compliance"},
	"computer-enrollment-prestage":                         {"computerEnrollmentPrestage.html", "PreStage enrollments"},
	"computer-invitations":                                 {"computerInvitations.html", "Enrollment invitations"},
	"computers":                                            {"computers.html", "Search inventory"},
	"computers/software-updates":                           {"view/computers/software-updates", "Software updates"},
	"dashboard":                                            {"", "Dashboard"},
	"devices/software-updates":                             {"view/devices/software-updates", "Software updates"},
	"ebooks":                                               {"eBooks.html", "eBooks"},
	"ios-configuration-profiles":                           {"iOSConfigurationProfiles.html", "Configuration profiles"},
	"licensed-software":                                    {"licensedSoftware.html", "Licensed software"},
	"licensed-software-templates":                          {"licensedSoftwareTemplates.html", "Licensed software templates"},
	"mac-apps":                                             {"macApps.html", "Mac apps"},
	"macos-configuration-profiles":                         {"OSXConfigurationProfiles.html", "Configuration profiles"},
	"mms-token-management":                                 {"view/mfe/mms-token-management", "Content token management"},
	"mobile-device-apps":                                   {"mobileDeviceApps.html", "Mobile device apps"},
	"mobile-device-enrollment-profiles":                    {"mobileDeviceEnrollmentProfiles.html", "Enrollment profiles"},
	"mobile-device-invitations":                            {"mobileDeviceInvitations.html", "Enrollment invitations"},
	"mobile-device-prestage":                               {"mobileDevicePrestage.html", "PreStage enrollments"},
	"mobile-devices":                                       {"mobileDevices.html", "Search inventory"},
	"notifications":                                        {"notifications.html", "Notifications"},
	"patch":                                                {"patch.html", "Patch management"},
	"patch-remote-sources":                                 {"patchRemoteSources.html", "Patch external source"},
	"peripheral-types":                                     {"peripheralTypes.html", "Peripheral types"},
	"peripherals":                                          {"peripherals.html", "Peripherals"},
	"policies":                                             {"policies.html", "Policies"},
	"provisioning-profiles":                                {"provisioningProfiles.html", "Provisioning profiles"},
	"restricted-software":                                  {"restrictedSoftware.html", "Restricted software"},
	"settings":                                             {"view/settings", "Settings"},
	"settings/accounts":                                    {"accounts.html", "User accounts and groups"},
	"settings/acknowledgements":                            {"view/settings/jamf-pro-information/acknowledgements", "Acknowledgements"},
	"settings/active-users-display":                        {"view/settings/system-settings/active-users-display", "Active users display"},
	"settings/air-play-permissions":                        {"airPlayPermissions.html", "AirPlay permissions"},
	"settings/api-roles-and-clients":                       {"view/settings/system-settings/api-roles-and-clients", "API roles and clients"},
	"settings/app-installers":                              {"view/settings/computer-management/app-installers", "App Installers"},
	"settings/app-request":                                 {"view/settings/self-service/app-request", "App Request"},
	"settings/apple-school-manager-instance":               {"appleSchoolManagerInstance.html", "Apple School Manager"},
	"settings/beyond-corp":                                 {"view/settings/global-management/conditional-access/beyond-corp", "BeyondCorp Enterprise integration"},
	"settings/bookmarks":                                   {"view/settings/self-service/bookmarks", "Bookmarks"},
	"settings/branding":                                    {"view/settings/self-service/branding", "Branding"},
	"settings/buildings":                                   {"view/settings/network-organization/buildings", "Buildings"},
	"settings/categories":                                  {"categories.html", "Categories"},
	"settings/change-management":                           {"changeManagement.html", "Change management"},
	"settings/change-management-logs":                      {"changeManagementLogs.html", "Change management logs"},
	"settings/check-in":                                    {"view/settings/computer-management/check-in", "Check-in"},
	"settings/cloud-distribution-point":                    {"view/settings/server-infrastructure/cloud-distribution-point", "Cloud distribution point"},
	"settings/cloud-hosted-services":                       {"view/settings/global-management/cloud-hosted-services", "Cloud Services connection"},
	"settings/cloud-idps":                                  {"view/settings/system-settings/cloud-idps", "Cloud identity providers"},
	"settings/clustering":                                  {"clustering.html", "Clustering"},
	"settings/computer-extension-attributes":               {"view/settings/computer-management/computer-extension-attributes", "Extension attributes"},
	"settings/computer-inventory-display-preferences":      {"computerInventoryDisplayPreferences.html", "Inventory display"},
	"settings/computer-security":                           {"computerSecurity.html", "Security"},
	"settings/conditional-access":                          {"ConditionalAccess.html", "Conditional access"},
	"settings/configurator-enrollment":                     {"configuratorEnrollment.html", "Apple Configurator enrollment"},
	"settings/departments":                                 {"view/settings/network-organization/departments", "Departments"},
	"settings/device-compliance":                           {"view/settings/global-management/conditional-access/device-compliance", "Device compliance"},
	"settings/device-enrollment-program-instances":         {"deviceEnrollmentProgramInstances.html", "Automated Device Enrollment"},
	"settings/directory-bindings":                          {"directoryBindings.html", "Directory bindings"},
	"settings/disk-encryptions":                            {"diskEncryptions.html", "Disk encryption configurations"},
	"settings/dock-items":                                  {"dockItems.html", "Dock items"},
	"settings/edu-feature-settings":                        {"eduFeatureSettings.html", "Apple education support"},
	"settings/enrollment":                                  {"view/settings/global-management/enrollment", "User-initiated enrollment"},
	"settings/enrollment-customization":                    {"view/settings/global-management/enrollment-customization", "Enrollment customization"},
	"settings/event-logs":                                  {"eventLogs.html", "Event logs"},
	"settings/file-share-distribution-points":              {"view/settings/server-infrastructure/file-share-distribution-points", "File share distribution points"},
	"settings/gsx-connection":                              {"view/settings/global-management/gsx-connection", "GSX connection"},
	"settings/ibeacons":                                    {"ibeacons.html", "iBeacons"},
	"settings/impact-alert-notifications":                  {"view/settings/system-settings/impact-alert-notifications", "Impact alert notifications"},
	"settings/infrastructure-managers":                     {"infrastructureManagers.html", "Infrastructure Managers"},
	"settings/inventory-collection":                        {"inventoryCollection.html", "Inventory collection"},
	"settings/inventory-preload":                           {"view/settings/global-management/inventory-preload", "Inventory preload"},
	"settings/ios-app-maintenance":                         {"iosAppMaintenance.html", "App maintenance"},
	"settings/jamf-connect":                                {"view/settings/jamf-applications/jamf-connect", "Jamf Connect"},
	"settings/jamf-parent":                                 {"view/settings/jamf-applications/jamf-parent", "Jamf Parent"},
	"settings/jamf-pro-url":                                {"jssServerURL.html", "Jamf Pro URL"},
	"settings/jamf-protect":                                {"view/settings/jamf-applications/jamf-protect", "Jamf Protect"},
	"settings/jamf-remote-assist":                          {"view/settings/jamf-applications/jamf-remote-assist", "Jamf Remote Assist"},
	"settings/jamf-teacher":                                {"view/settings/jamf-applications/jamf-teacher", "Jamf Teacher"},
	"settings/json-web-token-configs":                      {"jsonWebTokenConfigs.html", "JSON web token configuration"},
	"settings/ldap-servers":                                {"ldapServers.html", "LDAP servers"},
	"settings/license-information":                         {"view/settings/system-settings/license-information", "License information"},
	"settings/limited-access":                              {"limitedAccess.html", "Limited access"},
	"settings/logging":                                     {"logging.html", "Jamf Pro server logs"},
	"settings/login-customization":                         {"view/settings/system-settings/login-customization", "Login page"},
	"settings/mac-app-update-settings":                     {"macAppUpdateSettings.html", "App updates"},
	"settings/mac-onboarding":                              {"view/settings/self-service/mac-onboarding", "macOS Onboarding"},
	"settings/maintenance-screens":                         {"maintenanceScreens.html", "Maintenance pages"},
	"settings/mdm-profile-settings":                        {"view/settings/global-management/mdm-profile-settings", "MDM profile settings"},
	"settings/mobile-device-extension-attributes":          {"view/settings/device-management/mobile-device-extension-attributes", "Extension attributes"},
	"settings/mobile-device-inventory-collection":          {"mobileDeviceInventoryCollection.html", "Inventory collection"},
	"settings/mobile-device-inventory-display-preferences": {"mobileDeviceInventoryDisplayPreferences.html", "Inventory display"},
	"settings/mobile-device-self-service":                  {"mobileDeviceSelfService.html", "iOS"},
	"settings/network-integration":                         {"networkIntegration.html", "Network integration"},
	"settings/network-segments":                            {"networkSegments.html", "Network segments"},
	"settings/packages":                                    {"view/settings/computer-management/packages", "Packages"},
	"settings/password-policy":                             {"passwordPolicy.html", "Password policy"},
	"settings/patch-management-settings":                   {"patchManagementSettings.html", "Patch management"},
	"settings/pki-certificate-authorities":                 {"pkiCertificateAuthorities.html", "PKI certificates"},
	"settings/printers":                                    {"printers.html", "Printers"},
	"settings/push-notification-certificate":               {"pushNotificationCertificate.html", "Push certificates"},
	"settings/reenrollment":                                {"view/settings/global-management/reenrollment", "Re-enrollment"},
	"settings/remote-administration":                       {"view/settings/global-management/remote-administration", "Remote administration"},
	"settings/removable-mac-addresses":                     {"removableMACAddresses.html", "Removable MAC addresses"},
	"settings/retention-policies":                          {"retentionPolicies.html", "Log flushing"},
	"settings/scripts":                                     {"view/settings/computer-management/scripts", "Scripts"},
	"settings/self-service-macos":                          {"view/settings/self-service/self-service-macos", "macOS"},
	"settings/self-service-plus":                           {"view/settings/jamf-applications/self-service-plus", "Self Service+"},
	"settings/sites":                                       {"sites.html", "Sites"},
	"settings/smtp-server":                                 {"view/settings/system-settings/smtp-server", "SMTP server"},
	"settings/software-update-servers":                     {"softwareUpdateServers.html", "Software update servers"},
	"settings/sso":                                         {"view/settings/system-settings/sso", "Single sign-on"},
	"settings/status":                                      {"status.html", "Memory usage"},
	"settings/statusdb":                                    {"statusdb.html", "Database table summary"},
	"settings/summary":                                     {"summary.html", "Jamf Pro summary"},
	"settings/symantec-ca-configuration":                   {"symantecCaConfiguration.html", "Symantec CA configuration"},
	"settings/tomcat":                                      {"tomcat.html", "Apache Tomcat settings"},
	"settings/user-extension-attributes":                   {"userExtensionAttributes.html", "Extension attributes"},
	"settings/user-migration":                              {"userMigration.html", "User migration"},
	"settings/volume-purchasing":                           {"view/settings/global-management/volume-purchasing", "Volume purchasing"},
	"settings/webhooks":                                    {"webhooks.html", "Webhooks"},
	"smart-computer-groups":                                {"smartComputerGroups.html", "Smart computer groups"},
	"smart-mobile-device-groups":                           {"smartMobileDeviceGroups.html", "Smart device groups"},
	"smart-user-groups":                                    {"smartUserGroups.html", "Smart user groups"},
	"software-titles":                                      {"softwareTitles.html", "Patch management software titles"},
	"static-computer-groups":                               {"staticComputerGroups.html", "Static computer groups"},
	"static-mobile-device-groups":                          {"staticMobileDeviceGroups.html", "Static device groups"},
	"static-user-groups":                                   {"staticUserGroups.html", "Static user groups"},
	"user-contents":                                        {"userContents.html", "Search volume content"},
	"users":                                                {"users.html", "Search users"},
	"volume-purchase-program-assignments":                  {"volumePurchaseProgramAssignments.html", "Volume assignments"},
	"volume-purchase-program-invitations":                  {"volumePurchaseProgramInvitations.html", "Invitations"},
}
