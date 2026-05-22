# Bugfix Requirements Document

## Introduction

Web UI 完全不可用：HTML 模板中的 DOM ID、表单元素和字段名与 JavaScript 代码期望的不一致，导致初始化、登录、证书管理、机器管理等核心页面在浏览器中无法正常工作。后端 API 正常，但前端无法绑定事件、渲染数据或正确读取后端返回字段。共涉及 5 个 P1 级别问题。

## Bug Analysis

### Current Behavior (Defect)

**Bug 1: 初始化页和登录页模板 ID 与 JS 不一致**

1.1 WHEN user navigates to `/init` page THEN the system fails to bind the admin creation form because `init.js` looks for `init-admin-section`, `init-admin-form`, `admin-username`, `admin-password` but the HTML template provides `step-admin`, `admin-form`, `username`, `password`

1.2 WHEN user navigates to `/init` page and reaches config step THEN the system fails to bind the config form because `init.js` looks for `init-config-section`, `init-config-form`, `cfg-external-url`, `cfg-listen-addr`, `cfg-heartbeat-timeout`, `cfg-poll-interval`, `cfg-alert-days`, `cfg-certbot-path`, `cfg-certbot-datadir`, `cfg-certbot-email`, `cfg-readonly-enabled`, `cfg-readonly-password`, `cfg-monitor-port`, `cfg-monitor-interval` but the HTML template provides `step-config`, `config-form`, `readonly-password`, `certbot-email`, `domain-check-interval`, `cert-renew-days`

1.3 WHEN user navigates to `/login` page THEN the admin login form fails to read credentials because `login.js` looks for `username` and `password` element IDs but the HTML template provides `login-username` and `login-password`

1.4 WHEN user navigates to `/login` page and selects readonly login THEN the readonly login form fails to bind because `login.js` looks for `readonly-login-form` and `readonly-password` but the HTML template provides `readonly-form` and `readonly-pwd`

**Bug 2: 证书和机器页面模板与 JS 不一致**

1.5 WHEN certificates page loads THEN the certificate list renders to nothing because `certificates.js` targets `certificates-tbody` but the HTML template provides `certs-body`

1.6 WHEN user clicks upload/issue buttons on certificates page THEN no form appears because `certificates.js` listens on `upload-cert-form`, `issue-cloudflare-form`, `manual-dns-form` which do not exist in the HTML template (no modal/form markup present)

1.7 WHEN machines page loads THEN the machine list renders to nothing because `machines.js` targets `machines-tbody` but the HTML template provides `machines-body`

1.8 WHEN user clicks "添加机器" button THEN no form appears because `machines.js` listens on `create-machine-form` and `machine-filter-form` which do not exist in the HTML template (no modal/form markup present)

**Bug 3: 机器 Agent 安装命令不完整**

1.9 WHEN a machine is created successfully THEN the system only displays the raw `agent_token` without a complete install command containing server URL, machine ID, and token

1.10 WHEN user requests install command via GET `/api/machines/{id}/install-command` THEN the system returns a template with `<AGENT_TOKEN>` placeholder that the user must manually replace

1.11 WHEN user views machine certificate deploy configs THEN the system shows only a text input for certificate ID instead of a dropdown populated from `/api/certificates/` showing domain coverage and expiry

1.12 WHEN user views machine certificate deploy configs THEN the system does not display deployment logs from `/api/machines/{machine_id}/certificates/{mc_id}/deployment-logs`

**Bug 4: 手动 DNS 签发返回字段与前端读取字段不一致**

1.13 WHEN manual DNS challenge starts successfully THEN the TXT record name and value display as empty because the frontend reads `record_name` and `record_value` but the backend returns `txt_record_name` and `txt_record_value`

**Bug 5: Cloudflare DNS main_domains 为空被前端禁止**

1.14 WHEN user creates a Cloudflare DNS config with empty main_domains (to fetch all zones) THEN the system blocks submission because the frontend has `required` attribute on the main_domains input and validates `mainDomainsStr` is non-empty before submitting

### Expected Behavior (Correct)

**Bug 1 Fix: 初始化页和登录页模板 ID 统一**

2.1 WHEN user navigates to `/init` page THEN the system SHALL correctly bind the admin creation form by ensuring HTML template IDs match what `init.js` expects (`init-admin-section`, `init-admin-form`, `admin-username`, `admin-password`) OR by updating JS to match the HTML IDs

2.2 WHEN user navigates to `/init` page and reaches config step THEN the system SHALL correctly bind the config form with all required fields (`cfg-external-url`, `cfg-listen-addr`, `cfg-heartbeat-timeout`, `cfg-poll-interval`, `cfg-alert-days`, `cfg-certbot-path`, `cfg-certbot-datadir`, `cfg-certbot-email`, `cfg-readonly-enabled`, `cfg-readonly-password`, `cfg-monitor-port`, `cfg-monitor-interval`) present in the HTML template

2.3 WHEN user navigates to `/login` page THEN the system SHALL correctly read admin credentials by ensuring the username and password input IDs in HTML match what `login.js` expects

2.4 WHEN user navigates to `/login` page and selects readonly login THEN the system SHALL correctly bind the readonly form by ensuring the form ID and password input ID in HTML match what `login.js` expects

**Bug 2 Fix: 证书和机器页面模板与 JS 统一**

2.5 WHEN certificates page loads THEN the system SHALL render the certificate list into the correct `<tbody>` element by ensuring the HTML ID matches what `certificates.js` targets

2.6 WHEN user clicks upload/issue buttons on certificates page THEN the system SHALL display the appropriate form (upload form, Cloudflare issue form, or manual DNS form) either via modal or inline form elements that `certificates.js` can bind to

2.7 WHEN machines page loads THEN the system SHALL render the machine list into the correct `<tbody>` element by ensuring the HTML ID matches what `machines.js` targets

2.8 WHEN user clicks "添加机器" button THEN the system SHALL display a machine creation form (either via modal or inline) that `machines.js` can bind to with fields for name, IP, tags, and remark

**Bug 3 Fix: 机器 Agent 安装命令完整闭环**

2.9 WHEN a machine is created successfully THEN the system SHALL display a complete, copy-ready install command containing the server external URL, machine ID, and agent token

2.10 WHEN user requests install command via GET `/api/machines/{id}/install-command` THEN the system SHALL return a complete command with the actual token embedded (or the frontend SHALL assemble the complete command using the token from the creation/regeneration response)

2.11 WHEN user views machine certificate deploy configs THEN the system SHALL display a certificate dropdown populated from `/api/certificates/` showing certificate name, covered domains, and expiry date

2.12 WHEN user views machine certificate deploy configs THEN the system SHALL display recent deployment logs (up to 30 entries) fetched from `/api/machines/{machine_id}/certificates/{mc_id}/deployment-logs`

**Bug 4 Fix: 手动 DNS 字段名对齐**

2.13 WHEN manual DNS challenge starts successfully THEN the system SHALL correctly display the TXT record name and value by reading `txt_record_name` and `txt_record_value` from the backend response

**Bug 5 Fix: Cloudflare DNS 允许空 main_domains**

2.14 WHEN user creates a Cloudflare DNS config with empty main_domains THEN the system SHALL allow submission and send `main_domains: []` to the backend, enabling the "fetch all zones" behavior

### Unchanged Behavior (Regression Prevention)

3.1 WHEN user completes the full init flow (create admin → save config → redirect to login) THEN the system SHALL CONTINUE TO call the correct backend APIs (`POST /init/admin`, `POST /init/config`) with the correct request body format

3.2 WHEN user logs in with valid admin credentials THEN the system SHALL CONTINUE TO call `POST /api/auth/login` with `{username, password}` and store the returned token in localStorage

3.3 WHEN user logs in with valid readonly password THEN the system SHALL CONTINUE TO call `POST /api/auth/readonly-login` with `{password}` and store the returned token in localStorage

3.4 WHEN certificates page loads with existing certificates THEN the system SHALL CONTINUE TO call `GET /api/certificates/` and render certificate data (name, domains, source, expiry, auto_renew)

3.5 WHEN user uploads a certificate THEN the system SHALL CONTINUE TO call `POST /api/certificates/` with `{name, cert_pem, key_pem, chain_pem, auto_renew}` in base64-encoded format

3.6 WHEN user issues a certificate via Cloudflare THEN the system SHALL CONTINUE TO call `POST /api/certificates/issue/cloudflare` with `{name, domains, thirdpart_dns_id, auto_renew}`

3.7 WHEN user completes manual DNS verification THEN the system SHALL CONTINUE TO call `POST /api/certificates/issue/manual-dns/complete` with `{session_id, auto_renew}`

3.8 WHEN machines page loads with existing machines THEN the system SHALL CONTINUE TO call `GET /api/machines` and render machine data (name, ip, status, tags, agent_version, last_heartbeat_at)

3.9 WHEN user creates a machine THEN the system SHALL CONTINUE TO call `POST /api/machines` with `{name, ip, tags, remark}` and receive `agent_token` in the response

3.10 WHEN user creates a Cloudflare DNS config with specified main_domains THEN the system SHALL CONTINUE TO send the domains array and filter zones by those domains as before

3.11 WHEN user triggers certificate deployment to a machine THEN the system SHALL CONTINUE TO call `POST /api/machines/{id}/certificates/{mc_id}/deploy`

3.12 WHEN user deletes a certificate or machine THEN the system SHALL CONTINUE TO call the respective DELETE endpoints and refresh the list
