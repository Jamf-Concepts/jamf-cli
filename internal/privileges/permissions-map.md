---
updatedAt: 2026-09-03T13:00:10.000Z
---

Fetch the complete documentation index at: https://developer.jamf.com/platform-api/llms.txt. Use this file to discover all available pages before exploring further. Append .md to any documentation page URL to get its markdown version.

# Permissions map

Same permissions, new format.

## Format

```
{capability}:{action}
```

The capability comes first in kebab-case, followed by the action in lowercase. No product name appears in the string, because one capability is reached by endpoints across several products.

Six actions exist:

| Action    | Meaning                          |
| --------- | -------------------------------- |
| `create`  | Create a resource                |
| `read`    | Retrieve a resource              |
| `update`  | Modify a resource                |
| `delete`  | Remove a resource                |
| `deploy`  | Push a configuration to devices  |
| `execute` | Send a command, or run an action |

Actions are lowercase and case-sensitive. **Grant every action your integration uses.** `devices:update` covers writes only, so add `devices:read` if your integration reads a record before modifying it.

Any endpoint that sends a command to a device uses `execute`, including local admin password changes.

## Converting an old privilege name

Most old names convert mechanically. The capability token is the resource portion of the old privilege, and the action moves to the end.

| Old privilege      | Old beta slug                 | GA capability permission  |
| ------------------ | ----------------------------- | ------------------------- |
| Read Buildings     | `read:pro:buildings`          | `buildings:read`          |
| Create Departments | `create:pro:departments`      | `departments:create`      |
| Update Scripts     | `update:pro:scripts`          | `scripts:update`          |
| Change Password    | `execute:pro:change-password` | `change-password:execute` |

The exceptions below are the part worth reading. Several old privileges collapse into one capability, and one old privilege splits into two.

***

## Where the mapping is not one to one

### Computer and mobile privileges collapse into one device capability

The old model had a separate privilege for computers and for mobile devices. Those pairs are now single device-level capabilities. A caller cannot hold half of `devices:read`.

| Old privilege pair                                                  | GA capability              |
| ------------------------------------------------------------------- | -------------------------- |
| Computers + Mobile Devices                                          | `devices`                  |
| Computer Groups + Mobile Device Groups                              | `device-groups`            |
| Computer Extension Attributes + Mobile Device Extension Attributes  | `extension-attributes`     |
| macOS Configuration Profiles + Mobile Device Configuration Profiles | `configuration-profiles`   |
| Computer Invitations + Mobile Device Invitations                    | `enrollment-invitations`   |
| Advanced Computer Searches + Advanced Mobile Device Searches        | `advanced-device-searches` |
| Computer PreStage Enrollments + Mobile Device PreStage Enrollments  | `prestage-enrollments`     |

### Smart and static groups collapse

`device-groups` and `user-groups` each cover both the smart and the static privilege.

### Commands split into two capabilities

This is the one case where an old privilege becomes **more** granular, and it is deliberate. Erase, unmanage, and remove MDM profile destroy data or end management, so they are separated.

| Old privilege                              | GA capability                    | Covers                               |
| ------------------------------------------ | -------------------------------- | ------------------------------------ |
| Computer Commands + Mobile Device Commands | `device-actions:{r,d,x}`         | Routine commands, and command status |
| Computer Commands + Mobile Device Commands | `destructive-device-actions:{x}` | Erase, unmanage, remove MDM profile  |

### Umbrella capabilities absorb several privileges

These fold distinct concepts rather than a computer and mobile pair, so you lose the ability to grant one without the other.

| Old privileges                                                      | GA capability              | What you can no longer grant separately |
| ------------------------------------------------------------------- | -------------------------- | --------------------------------------- |
| Mac Applications + Mobile Device Applications                       | `applications`             | Mac apps without mobile apps            |
| Jamf Connect Settings + Jamf Connect Deployments + Deployment Retry | `jamf-connect-deployments` | Settings without deployments            |
| Jamf Protect Settings + Jamf Protect Deployments + Deployment Retry | `jamf-protect-deployments` | Settings without deployments            |
| Self Service Branding Configuration + Self Service                  | `self-service`             | Branding without Self Service           |

### One reassignment

`PUT /local-admin-password/{clientManagementId}/set-password` previously required Computer Commands plus Mobile Device Commands. It now sits under `local-admin-passwords`, chosen by subject matter rather than by mechanism.

***

## Endpoints that need two capabilities

Where two old privileges map to two different capabilities, both are still required.

| Endpoint                                                             | Requires                                                      |
| -------------------------------------------------------------------- | ------------------------------------------------------------- |
| `POST /computers`, `POST /mobile-devices` and their update analogues | `devices` + `users`                                           |
| `/patchpolicies` read                                                | `patch-policies:{r}` + `patch-management-software-titles:{r}` |
| `/categories` list                                                   | `categories:{r}` + `self-service:{r}`                         |
| `/slasa` accept                                                      | `jss-information` + `activation-code:{u}`                     |
| `POST /mobiledevicecommands/command`                                 | `destructive-device-actions:{x}` + `devices:{c}`              |
| `/gsx-connection`                                                    | `push-certificates:{r}` + `gsx-connection:{r}`                |
| `/mobile-device-groups/static-group-membership/{id}/assignments`     | `device-groups:{r}` + `devices:{r}`                           |

One endpoint accepts either of two capabilities rather than requiring both: `/logflush` takes `flush-policy-logs:{x}` **or** `policies:{d}`. Jamf Pro enforced those inconsistently for the same action, so requiring both would reject callers Jamf Pro itself allows.

***

## Find the capability for an endpoint you already call

Action codes: `c` create, `r` read, `u` update, `d` delete, `dep` deploy, `x` execute.

The **Endpoints** column lists resource-root prefixes rather than every endpoint. A capability owns all paths under its roots. Where roots overlap, the longer prefix wins, so `/computers-inventory` is `devices` while `/computers-inventory/{id}/filevault` is `disk-encryption-recovery-key`.

Version segments are collapsed. `/jcds` covers `/v1/jcds` and later versions.

### Organization management scope

| Permission                | Capability                    | Endpoints                                 |
| ------------------------- | ----------------------------- | ----------------------------------------- |
| Licensing                 | `licensing:{r}`               | Account `/licensing/v*/licenses`          |
| Partner deal registration | `deal-registration:{c,r}`     | Account `/partners/v*/deal-registrations` |
| Distributor actions       | `distributor-actions:{c,r,u}` | Account `/partners/v*/distributor`        |
| SSO connections           | `sso-connections:{c,r,u,d}`   | Account `/sso/v*/connections`             |
| SSO domains               | `sso-domains:{c,r,u,d}`       | Account `/sso/v*/domains`                 |

### Inventory

| Permission                  | Capability                            | Endpoints                                                                                                                                                                                                                                                                                                                                                                                          |
| --------------------------- | ------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Devices                     | `devices:{c,r,u,d}`                   | Platform `/devices`, `/devices/{id}/applications`, `/users/{id}/devices`, `/devices/{id}/users` · Pro `/computers-inventory`, `/computers-inventory-detail`, `/preview/computers`, `/ddm/{id}/status-items`, `/mobile-devices` · Classic `/computers`, `/mobiledevices` · Protect `getComputer`, `listComputers`, `requestComputerTimeline`, `updateComputer`, `deleteComputer`, `setComputerPlan` |
| Device groups               | `device-groups:{c,r,u,d}`             | Platform `/device-groups`, `/device-groups/{id}/members`, `/devices/{id}/device-groups` · Pro `/computer-groups`, `/computers/{id}/recalculate-smart-groups`, `/devices/{id}/groups`, `/groups`, `/mobile-device-groups`, `/smart-computer-groups`, `/smart-mobile-device-groups` · Classic `/computergroups`, `/mobiledevicegroups` · Security Cloud `/groups`                                    |
| Users                       | `users:{c,r,u,d}`                     | Pro `/users` · Classic `/users` · Protect `listUsers`, `deleteUser`                                                                                                                                                                                                                                                                                                                                |
| User groups                 | `user-groups:{c,r,u,d}`               | Pro `/smart-user-groups`, `/static-user-groups` · Classic `/usergroups`                                                                                                                                                                                                                                                                                                                            |
| Device extension attributes | `extension-attributes:{c,r,u,d}`      | Pro `/computer-extension-attributes`, `/devices/extensionAttributes`, `/mobile-device-extension-attributes` · Classic `/computerextensionattributes`, `/mobiledeviceextensionattributes`                                                                                                                                                                                                           |
| User extension attributes   | `user-extension-attributes:{c,r,u,d}` | Classic `/userextensionattributes`                                                                                                                                                                                                                                                                                                                                                                 |
| Advanced device searches    | `advanced-device-searches:{c,r,u,d}`  | Pro `/advanced-mobile-device-searches` · Classic `/advancedcomputersearches`, `/advancedmobiledevicesearches`, `/computerapplications`, `/savedsearches`                                                                                                                                                                                                                                           |
| Advanced user searches      | `advanced-user-searches:{c,r,u,d}`    | Pro `/advanced-user-content-searches` · Classic `/advancedusersearches`                                                                                                                                                                                                                                                                                                                            |
| Device history              | `device-history:{r}`                  | Classic `/computerhistory`, `/computerapplicationusage`, `/computerhardwaresoftwarereports`, `/computermanagement`, `/computerreports`, `/mobiledevicehistory`                                                                                                                                                                                                                                     |

### Organizational context

| Permission       | Capability                   | Endpoints                                   |
| ---------------- | ---------------------------- | ------------------------------------------- |
| Sites            | `sites:{c,r,u,d}`            | Pro `/sites` · Classic `/sites`             |
| Buildings        | `buildings:{c,r,u,d}`        | Pro `/buildings` · Classic `/buildings`     |
| Departments      | `departments:{c,r,u,d}`      | Pro `/departments` · Classic `/departments` |
| Categories       | `categories:{c,r,u,d}`       | Pro `/categories` · Classic `/categories`   |
| Classes          | `classes:{c,r,u,d}`          | Classic `/classes`                          |
| Network segments | `network-segments:{c,r,u,d}` | Classic `/networksegments`                  |
| iBeacon regions  | `ibeacon:{c,r,u,d}`          | Classic `/ibeacons`                         |

Listing categories also needs `self-service:{r}`.

### Device actions

| Permission                 | Capability                       | Endpoints                                                                                                                                                                                                                                                                                                                                                                                                                         |
| -------------------------- | -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Device actions             | `device-actions:{r,d,x}`         | Platform `/devices/{id}/check-in`, `/restart`, `/shutdown` · Pro `/deploy-package`, `/ddm/{id}/sync`, `/mdm`, `/mdm-renewal`, `/apns-client-push-status`, `/jamf-management-framework/redeploy`, `/macos-managed-software-updates` · Classic `GET /computercommands`, `GET /mobiledevicecommands`, `DELETE /commandflush`, `POST /mobiledevicecommands/command/DeviceName`, `POST /mobiledevicecommands/command/ScheduleOSUpdate` |
| Destructive device actions | `destructive-device-actions:{x}` | Platform `/devices/{id}/erase`, `/devices/{id}/unmanage` · Pro `/computers-inventory/{id}/erase`, `/remove-mdm-profile`, `/mobile-devices/{id}/erase`, `/mobile-devices/{id}/unmanage`, `/mobile-device-groups/{id}/erase` · Classic `POST /computercommands/command`, `POST /mobiledevicecommands/command`, `POST /mobiledevicecommands/name`                                                                                    |

### Device secrets

| Permission                   | Capability                         | Endpoints                                                                              |
| ---------------------------- | ---------------------------------- | -------------------------------------------------------------------------------------- |
| FileVault recovery key       | `disk-encryption-recovery-key:{r}` | Pro `/computers-inventory/filevault`, `/{id}/filevault`                                |
| Recovery lock password       | `recovery-lock:{r}`                | Pro `/computers-inventory/{id}/view-recovery-lock-password`                            |
| Device lock PIN              | `computer-device-lock-pin:{r}`     | Pro `/computers-inventory/{id}/view-device-lock-pin`                                   |
| Local Admin Passwords (LAPS) | `local-admin-passwords:{r,u,x}`    | Pro `/local-admin-password`, `/local-admin-password/{clientManagementId}/set-password` |

### Deployment

| Permission              | Capability                                 | Endpoints                                                                                                                       |
| ----------------------- | ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------- |
| Blueprints              | `blueprints:{c,r,u,d,dep}`                 | Platform `/blueprints`, `/blueprints/{id}/deploy` and `/undeploy` (`{dep}`), `/blueprints/{id}/report`, `/blueprint-components` |
| Declarations reporting  | `declarations:{r}`                         | Platform `/ddm/report/devices`, `/ddm/report/declarations` · Pro `/dss-declarations`                                            |
| Configuration profiles  | `configuration-profiles:{c,r,u,d}`         | Classic `/osxconfigurationprofiles`, `/mobiledeviceconfigurationprofiles`                                                       |
| Policies                | `policies:{c,r,u,d}`                       | Pro `/policy-properties`, `/settings/obj/policyProperties` · Classic `/policies`                                                |
| Scripts                 | `scripts:{c,r,u,d}`                        | Pro `/scripts` · Classic `/scripts`                                                                                             |
| Packages                | `packages:{c,r,u,d}`                       | Pro `/packages` · Classic `/packages`                                                                                           |
| Printers                | `printers:{c,r,u,d}`                       | Classic `/printers`                                                                                                             |
| Dock items              | `dock-items:{c,r,u,d}`                     | Pro `/dock-items` · Classic `/dockitems`                                                                                        |
| Software updates        | `managed-software-updates:{c,r,u}`         | Pro `/managed-software-updates`                                                                                                 |
| Disk encryption         | `disk-encryption-configurations:{c,r,u,d}` | Classic `/diskencryptionconfigurations`                                                                                         |
| Directory bindings      | `directory-bindings:{c,r,u,d}`             | Classic `/directorybindings`                                                                                                    |
| Jamf Connect deployment | `jamf-connect-deployments:{r,u,dep}`       | Pro `/jamf-connect`, `/jamf-connect/config-profiles`, `/deployments/{id}/tasks`                                                 |
| Jamf Protect deployment | `jamf-protect-deployments:{r,u,dep}`       | Pro `/jamf-protect`, `/jamf-protect/register`, `/jamf-protect/history`                                                          |

### Enrollment

| Permission             | Capability                         | Endpoints                                                                           |
| ---------------------- | ---------------------------------- | ----------------------------------------------------------------------------------- |
| PreStage enrollments   | `prestage-enrollments:{c,r,u,d}`   | Pro `/computer-prestages`, `/mobile-device-prestages`                               |
| Enrollment profiles    | `enrollment-profiles:{c,r,u,d}`    | Pro `/mobile-device-enrollment-profile` · Classic `/mobiledeviceenrollmentprofiles` |
| Enrollment invitations | `enrollment-invitations:{c,r,u,d}` | Classic `/computerinvitations`, `/mobiledeviceinvitations`                          |
| Activation profiles    | `activation-profiles:{c,r,u,d}`    | Security Cloud `/activation-profiles`                                               |

### App lifecycle management

| Permission              | Capability                                   | Endpoints                                                                                                                                                                |
| ----------------------- | -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Apps                    | `applications:{c,r,u,d}`                     | Classic `/macapplications`, `/mobiledeviceapplications`                                                                                                                  |
| App package information | `jamf-packages-action:{r}`                   | Pro `/jamf-package`                                                                                                                                                      |
| Volume purchasing       | `volume-purchasing-locations:{c,r,u,d}`      | Pro `/volume-purchasing-locations`, `/volume-purchasing-subscriptions` · Classic `/vppaccounts`, `/vppassignments`, `/vppinvitations`                                    |
| eBooks                  | `ebooks:{c,r,u,d}`                           | Pro `/ebooks` · Classic `/ebooks`                                                                                                                                        |
| Provisioning profiles   | `provisioning-profiles:{c,r,u,d}`            | Classic `/mobiledeviceprovisioningprofiles`                                                                                                                              |
| Licensed software       | `licensed-software:{c,r,u,d}`                | Classic `/licensedsoftware`                                                                                                                                              |
| Restricted software     | `restricted-software:{c,r,u,d}`              | Classic `/restrictedsoftware`                                                                                                                                            |
| Patch policies          | `patch-policies:{c,r,u,d}`                   | Pro `/patch-policies` · Classic `/patchpolicies`                                                                                                                         |
| Patch titles            | `patch-management-software-titles:{c,r,u,d}` | Pro `/patch-software-title-configurations`, `/patch-management-accept-disclaimer` · Classic `/patchsoftwaretitles`, `/patchavailabletitles`, `/patchreports`, `/patches` |
| External patch sources  | `patch-external-source:{c,r,u,d}`            | Classic `/patchexternalsources`                                                                                                                                          |
| Internal patch sources  | `patch-internal-source:{r}`                  | Classic `/patchinternalsources`                                                                                                                                          |

Reading `/patchpolicies` also needs `patch-management-software-titles:{r}`.

### Compliance

| Permission                           | Capability                          | Endpoints                                                                                                                             |
| ------------------------------------ | ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| AI policies                          | `ai-policies:{c,r,u,d}`             | Platform `/policies` (and `/{policyId}`, `/versions`, `/deployment`, `/publish`), `/tools`, `/tools/{toolId}/schemas/{schemaVersion}` |
| Compliance Benchmarks                | `compliance-benchmarks:{c,r,d}`     | Platform `/benchmarks`, `/benchmarks/{id}`, `/benchmarks/{id}/rules`, `/{id}/devices`, `/{id}/compliance-percentage`                  |
| Compliance Benchmarks baseline rules | `compliance-benchmarks:{r}`         | Platform `/baselines`, `/rules?baselineId=`                                                                                           |
| Conditional access device compliance | `device-compliance-information:{r}` | Pro `/conditional-access/device-compliance-information/{computer\|mobile}/{id}`                                                       |

Compliance Benchmarks has no update action.

### Endpoint security

Reached through the Jamf Protect API. Permissions map to GraphQL operation names rather than paths.

| Permission                 | Capability                          | Operations                                                                                                                                       |
| -------------------------- | ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| Protection plans           | `protection-plans:{r,u}`            | `getPlan`, `listPlans`                                                                                                                           |
| Detection analytics        | `detection-analytics:{r,u}`         | `getAnalytic`, `listAnalytics`, `updateAnalyticSet`                                                                                              |
| Threat alerts              | `threat-alerts:{r,u}`               | `getAlert`, `listAlerts`, `updateAlerts`                                                                                                         |
| Prevent lists              | `prevent-lists:{c,r,u,d}`           | `getPreventList`, `listPreventLists`, `createPreventList`, `updatePreventList`, `deletePreventList`                                              |
| Threat definition versions | `threat-definition-versions:{r}`    | `listThreatPreventionVersions`                                                                                                                   |
| Unified logging filters    | `unified-logging-filters:{c,r,u,d}` | `getUnifiedLoggingFilter`, `listUnifiedLoggingFilters`, `createUnifiedLoggingFilter`, `updateUnifiedLoggingFilter`, `deleteUnifiedLoggingFilter` |
| Security audit log         | `security-audit-log:{r}`            | `listAuditLogsByDate`, `listAuditLogsByOp`, `listAuditLogsByUser`                                                                                |

### Secure enterprise access

| Permission                       | Capability                         | Endpoints                                      |
| -------------------------------- | ---------------------------------- | ---------------------------------------------- |
| Zero-Trust Network Access (ZTNA) | `ztna:{c,r,u,d}`                   | Security Cloud `/ztna`, `/dns/zones`           |
| Search domains                   | `search-domains:{r,u,d}`           | Security Cloud `/dns/search-domains`           |
| Custom hostname mappings         | `custom-hostname-mappings:{r,u,d}` | Security Cloud `/dns/custom-hostname-mappings` |
| Content categories               | `content-categories:{r}`           | Security Cloud `/categories`                   |

### Admin identity and access

| Permission            | Capability                | Endpoints                                                                                                             |
| --------------------- | ------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| Audit events          | `audit:{r}`               | Platform `/audit`                                                                                                     |
| Admin account         | `accounts:{c,r,u,d}`      | Pro `/accounts`, `/user` · Classic `/accounts`, `/jssuser`                                                            |
| Change admin password | `change-password:{x}`     | Pro `/user/change-password`                                                                                           |
| Admin account groups  | `account-groups:{r}`      | Pro `/account-groups`                                                                                                 |
| LDAP / cloud IdP      | `ldap-servers:{c,r,u,d}`  | Pro `/ldap`, `/ldap-keystore`, `/cloud-ldaps`, `/classic-ldap`, `/cloud-azure`, `/cloud-idp` · Classic `/ldapservers` |
| Single Sign-On        | `sso-settings:{r,u}`      | Pro `/sso`, `/sso/cert`, `/sso/dependencies`, `/sso/metadata/download`, `/oidc`                                       |
| Access management     | `access-management:{r,u}` | Pro `/enrollment/access-management`                                                                                   |
| Admin user sessions   | `user-sessions:{r}`       | Pro `/last-login`, `/user-sessions`                                                                                   |

### Admin file uploads

| Permission         | Capability         | Endpoints                                       |
| ------------------ | ------------------ | ----------------------------------------------- |
| Admin file uploads | `file-uploads:{c}` | Classic `/fileuploads/{resource}/{idType}/{id}` |

One endpoint attaches files to many object types under this single capability.

### Global settings

| Permission                              | Capability                                     | Endpoints                                                                                                       |
| --------------------------------------- | ---------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| UEM Connect configuration               | `uem-connect:{c,r,u,d}`                        | Security Cloud `/uem-connect`                                                                                   |
| Intune conditional access configuration | `conditional-access:{r}`                       | Pro `/conditional-access/device-compliance/feature-toggle`                                                      |
| Self Service configuration              | `self-service:{c,r,u,d}`                       | Pro `/self-service`, `/self-service-plus`                                                                       |
| App request settings                    | `app-request:{r,u}`                            | Pro `/app-request`, `/app-request/settings`, `/form-input-fields`                                               |
| Onboarding configuration                | `onboarding:{r,u}`                             | Pro `/onboarding`, `/onboarding/eligible-apps`, `/onboarding/history`                                           |
| Re-enrollment settings                  | `re-enrollment:{r,u}`                          | Pro `/reenrollment`, `/reenrollment/history`                                                                    |
| Return to service configuration         | `return-to-service:{r,u,d}`                    | Pro `/return-to-service`, `/return-to-service/{id}`                                                             |
| User-initiated enrollment settings      | `user-initiated-enrollment:{r,u}`              | Pro `/enrollment`, `/enrollment/access-groups`, `/adue-session-token-settings`, `/service-discovery-enrollment` |
| Apple Configurator enrollment settings  | `apple-configurator-enrollment:{r,u}`          | Pro `/supervision-identities`                                                                                   |
| Enrollment customization                | `enrollment-customization:{c,r,u,d}`           | Pro `/enrollment-customization`, `/enrollment-customizations`                                                   |
| Teacher app settings                    | `teacher-app:{r,u}`                            | Pro `/teacher-app`, `/teacher-app/history`                                                                      |
| Parent app settings                     | `parent-app:{r,u}`                             | Pro `/parent-app`, `/parent-app/history`                                                                        |
| Remote Assist                           | `remote-assist:{r}`                            | Pro `/jamf-remote-assist/session`                                                                               |
| TeamViewer configuration                | `remote-administration:{c,r,u,d}`              | Pro `/preview/remote-administration-configurations`                                                             |
| Device check-in configuration           | `computer-check-in:{r,u}`                      | Pro `/check-in` · Classic `/computercheckin`                                                                    |
| Device inventory collection settings    | `computer-inventory-collection-settings:{r,u}` | Pro `/computer-inventory-collection-settings` · Classic `/computerinventorycollection`                          |
| Inventory collection custom file paths  | `custom-paths:{c,d}`                           | Pro `/computer-inventory-collection-settings/custom-path`                                                       |
| Removable MAC addresses                 | `removable-mac-address:{c,r,u,d}`              | Classic `/removablemacaddresses`                                                                                |
| Inventory preload                       | `inventory-preload-records:{c,r,u,d}`          | Pro `/inventory-preload`                                                                                        |
| MDM profile renewal settings            | `mdm-profile-renewal-settings:{r,u}`           | Pro `/device-communication-settings`                                                                            |
| Notification settings                   | `impact-alert-notification-settings:{r,u}`     | Pro `/impact-alert-notification-settings`                                                                       |
| Dismiss notifications                   | `dismiss-notifications:{x}`                    | Pro `/notifications/{type}/{id}`                                                                                |
| Login disclaimer                        | `login-disclaimer:{u}`                         | Pro `/login-customization`                                                                                      |
| Webhooks                                | `webhooks:{c,r,u,d}`                           | Classic `/webhooks`                                                                                             |
| Allowed file upload extensions          | `allowed-file-extension:{c,r,d}`               | Classic `/allowedfileextensions`                                                                                |

### Infrastructure

| Permission                             | Capability                                      | Endpoints                                                                                           |
| -------------------------------------- | ----------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| Automated Device Enrollment connection | `device-enrollment-program-instances:{c,r,u,d}` | Pro `/device-enrollments`                                                                           |
| PKI certificates                       | `pki:{r,u}`                                     | Pro `/pki/certificate-authority/{id}`, `/pki/venafi`                                                |
| AD Certificate Services connector      | `ad-cs-settings:{c,r,u,d}`                      | Pro `/pki/adcs-settings`                                                                            |
| DigiCert Trust Lifecycle Manager       | `digicert-settings:{c,r,u,d}`                   | Pro `/pki/digicert/trust-lifecycle-manager`                                                         |
| APNS certificate                       | `push-certificates:{r,u}`                       | Pro `/gsx-connection`                                                                               |
| Apple GSX connection                   | `gsx-connection:{r,u}`                          | Pro `/gsx-connection`, `/gsx-connection/history`, `/gsx-connection/test` · Classic `/gsxconnection` |
| Distribution points                    | `distribution-points:{c,r,u,d}`                 | Pro `/distribution-points` · Classic `/distributionpoints`                                          |
| Cloud Distribution Point               | `cloud-distribution-point:{r,u}`                | Pro `/cloud-distribution-point`                                                                     |
| Jamf Cloud Distribution Service files  | `jamf-cloud-distribution-service-files:{c,r,d}` | Pro `/jcds/files`, `/jcds`                                                                          |
| JSON web token configuration           | `json-web-token-configuration:{c,r,u,d}`        | Classic `/jsonwebtokenconfigurations`                                                               |
| Software update servers                | `software-update-servers:{c,r,u,d}`             | Classic `/softwareupdateservers`                                                                    |
| SMTP                                   | `smtp-server:{r,u}`                             | Pro `/smtp-server` · Classic `/smtpserver`                                                          |
| Cache                                  | `cache:{r,u}`                                   | Pro `/cache-settings`                                                                               |
| Jamf Cloud Services connection         | `cloud-services-settings:{r,u}`                 | Pro `/csa/token`                                                                                    |
| Tomcat server                          | `apache-tomcat-settings:{u}`                    | Pro `/settings/issueTomcatSslCertificate`                                                           |
| Infrastructure Manager instances       | `infrastructure-managers:{c,r,u,d}`             | Classic `/infrastructuremanager`, `/healthcarelistener`, `/healthcarelistenerrule`                  |
| Retention policy                       | `retention-policy:{r,u}`                        | Pro `/log-flushing`, `/log-flushing/task`                                                           |
| Log flushing                           | `flush-policy-logs:{x}`                         | Classic `/logflush`                                                                                 |
| Activation code                        | `activation-code:{r,u}`                         | Pro `/activation-code` · Classic `/activationcode`                                                  |
| Jamf Pro SLASA                         | `jss-information:{r}`                           | Pro `/slasa`                                                                                        |
| M2M tenant ID                          | `m2m:{r}`                                       | Pro `/m2m/tenant-id`                                                                                |
| Jamf Pro server URL                    | `jss-url:{r,u}`                                 | Pro `/jamf-pro-server-url`, `/jamf-pro-server-url/history`                                          |

***

## Endpoints with no permission

`/jamf-pro-information` and `/jamf-pro-version` are unauthenticated and need no capability. The `/notifications` list is also unauthenticated, although dismissing a notification needs `dismiss-notifications:{x}`.

## Resources with no capability

| Resource                                                 | Reason                  |
| -------------------------------------------------------- | ----------------------- |
| Personal device profiles (BYOD)                          | Deprecated              |
| Managed preference profiles (legacy MCX)                 | Deprecated              |
| Peripherals and peripheral types                         | Deprecated              |
| macOS compliance baselines (Protect `listBaselineRules`) | Deprecated              |
| Jamf Pro API roles, API privileges, API integrations     | Managed in Jamf Account |

## Related articles

* [Platform API fundamentals](https://developer.jamf.com/platform-api/reference/platform-api-fundamentals): auth, scope levels, regions, pagination, and errors
* [Move an existing integration](https://developer.jamf.com/platform-api/reference/move-an-existing-integration): what to change in an existing Jamf Pro or Jamf Protect integration