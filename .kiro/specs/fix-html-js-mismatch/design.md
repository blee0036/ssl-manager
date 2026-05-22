# Fix HTML/JS Mismatch Bugfix Design

## Overview

Web UI 完全不可用：5 个 HTML 模板中的 DOM ID、表单元素和字段名与 JavaScript 代码期望的不一致，导致初始化、登录、证书管理、机器管理等核心页面在浏览器中无法正常工作。修复策略是更新 HTML 模板以匹配 JS 期望（JS 已在前一轮 bugfix 中修正为匹配后端 API），同时修复前端 JS 中的字段名读取错误（DNS challenge）和验证逻辑错误（main_domains required）。对于安装命令，增强前端在创建/重新生成 token 时直接拼装完整命令。

## Glossary

- **Bug_Condition (C)**: HTML 模板中的 DOM ID/结构与 JS 代码 `getElementById` 调用不匹配，或 JS 读取的 JSON 字段名与后端返回不一致，或前端验证阻止合法操作
- **Property (P)**: 所有页面的 DOM 元素 ID 与 JS 代码期望一致，表单能正确绑定事件并提交数据，字段名与后端 API 返回一致
- **Preservation**: 后端 API 调用方式（URL、HTTP 方法、请求体格式）不变，已有的正确 JS 逻辑不变
- **init.js**: `web/static/js/init.js` 中的初始化页面逻辑，期望 `init-admin-section`, `init-admin-form`, `admin-username`, `admin-password`, `init-config-section`, `init-config-form`, `cfg-*` 系列 ID
- **login.js**: `web/static/js/login.js` 中的登录页面逻辑，期望 `login-form`, `username`, `password`, `readonly-login-form`, `readonly-password`
- **certificates.js**: `web/static/js/certificates.js` 中的证书管理逻辑，期望 `certificates-tbody`, `upload-cert-form`, `issue-cloudflare-form`, `manual-dns-form`
- **machines.js**: `web/static/js/machines.js` 中的机器管理逻辑，期望 `machines-tbody`, `create-machine-form`, `machine-filter-form`
- **thirdpart-dns.js**: `web/static/js/thirdpart-dns.js` 中的 DNS 配置逻辑

## Bug Details

### Bug Condition

The bug manifests across 5 distinct areas where the HTML templates provide different DOM IDs/structures than what the JavaScript code expects, or where the JS reads wrong field names from backend responses, or where frontend validation blocks legitimate operations.

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type {page: string, action: string, context: object}
  OUTPUT: boolean

  // Bug 1: init.html IDs don't match init.js expectations
  IF input.page == "init" AND input.action == "bind_form"
    RETURN getElementById("init-admin-section") == null   // HTML has "step-admin"
           OR getElementById("init-admin-form") == null   // HTML has "admin-form"
           OR getElementById("admin-username") == null    // HTML has "username"
           OR getElementById("admin-password") == null    // HTML has "password"
           OR getElementById("init-config-section") == null  // HTML has "step-config"
           OR getElementById("init-config-form") == null     // HTML has "config-form"
           OR getElementById("cfg-external-url") == null     // not in HTML
           OR getElementById("cfg-listen-addr") == null      // not in HTML
           OR getElementById("cfg-heartbeat-timeout") == null // not in HTML
           OR getElementById("cfg-poll-interval") == null    // not in HTML
           OR getElementById("cfg-alert-days") == null       // not in HTML
           OR getElementById("cfg-certbot-path") == null     // not in HTML
           OR getElementById("cfg-certbot-datadir") == null  // not in HTML
           OR getElementById("cfg-certbot-email") == null    // not in HTML
           OR getElementById("cfg-readonly-enabled") == null // not in HTML
           OR getElementById("cfg-readonly-password") == null // not in HTML
           OR getElementById("cfg-monitor-port") == null     // not in HTML
           OR getElementById("cfg-monitor-interval") == null // not in HTML

  // Bug 1: login.html IDs don't match login.js expectations
  IF input.page == "login" AND input.action == "bind_form"
    RETURN getElementById("username") == null             // HTML has "login-username"
           OR getElementById("password") == null          // HTML has "login-password"
           OR getElementById("readonly-login-form") == null  // HTML has "readonly-form"
           OR getElementById("readonly-password") == null    // HTML has "readonly-pwd"

  // Bug 2: certificates.html IDs don't match certificates.js expectations
  IF input.page == "certificates" AND input.action == "render_list"
    RETURN getElementById("certificates-tbody") == null   // HTML has "certs-body"
  IF input.page == "certificates" AND input.action == "bind_forms"
    RETURN getElementById("upload-cert-form") == null     // not in HTML at all
           OR getElementById("issue-cloudflare-form") == null  // not in HTML
           OR getElementById("manual-dns-form") == null        // not in HTML

  // Bug 2: machines.html IDs don't match machines.js expectations
  IF input.page == "machines" AND input.action == "render_list"
    RETURN getElementById("machines-tbody") == null       // HTML has "machines-body"
  IF input.page == "machines" AND input.action == "bind_forms"
    RETURN getElementById("create-machine-form") == null  // not in HTML
           OR getElementById("machine-filter-form") == null  // not in HTML

  // Bug 3: install command incomplete
  IF input.page == "machines" AND input.action == "show_install_command"
    RETURN input.context.displayed_command CONTAINS "<AGENT_TOKEN>"
           OR input.context.displayed_command == raw_token_only

  // Bug 4: DNS challenge field name mismatch
  IF input.page == "certificates" AND input.action == "show_dns_challenges"
    RETURN input.context.read_field == "record_name"     // backend returns "txt_record_name"
           OR input.context.read_field == "record_value" // backend returns "txt_record_value"

  // Bug 5: main_domains required attribute blocks empty submission
  IF input.page == "thirdpart-dns" AND input.action == "submit_config"
    RETURN input.context.main_domains_input HAS "required" attribute
           OR validation_rejects_empty_mainDomainsStr

  RETURN false
END FUNCTION
```

### Examples

- **Init page**: User navigates to `/init`, `init.js` calls `getElementById('init-admin-section')` → returns `null` because HTML has `id="step-admin"` → admin form never displays/binds
- **Login page**: User submits login form, `login.js` reads `getElementById('username').value` → throws TypeError because HTML has `id="login-username"` → login impossible
- **Certificates page**: `certificates.js` calls `getElementById('certificates-tbody')` → returns `null` because HTML has `id="certs-body"` → certificate list never renders
- **Machines page**: `machines.js` calls `getElementById('machines-tbody')` → returns `null` because HTML has `id="machines-body"` → machine list never renders
- **Machine creation**: After successful `POST /api/machines`, frontend shows raw `agent_token` string without server URL or machine ID → user cannot copy-paste a working install command
- **Manual DNS**: Backend returns `{txt_record_name: "_acme-challenge.example.com", txt_record_value: "abc123"}`, frontend reads `ch.record_name` → displays empty string
- **Cloudflare DNS config**: User wants to create "fetch all zones" config with empty main_domains → `required` attribute on input prevents form submission

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- All backend API endpoints remain unchanged (URLs, methods, request/response formats)
- `init.js` logic for calling `POST /init/admin` and `POST /init/config` with correct body format
- `login.js` logic for calling `POST /api/auth/login` and `POST /api/auth/readonly-login`
- `certificates.js` logic for calling `GET /api/certificates/`, `POST /api/certificates/`, `POST /api/certificates/issue/cloudflare`, `POST /api/certificates/issue/manual-dns/start`, `POST /api/certificates/issue/manual-dns/complete`
- `machines.js` logic for calling `GET /api/machines`, `POST /api/machines`, `DELETE /api/machines/{id}`, `POST /api/machines/{id}/regenerate-token`, `POST /api/machines/{id}/certificates/{mc_id}/deploy`
- `thirdpart-dns.js` logic for calling `GET /api/thirdpart-dns`, `POST /api/thirdpart-dns`, `PUT /api/thirdpart-dns/{id}`
- CSS styling and layout structure of all pages
- Tab switching logic on login page
- Modal/toast utility functions in `app.js`

**Scope:**
All inputs that do NOT involve DOM ID lookups, form binding, field name reading, or the specific validation logic are completely unaffected. The JS business logic (API calls, data transformation, error handling) remains identical.

## Hypothesized Root Cause

Based on the code review, the root causes are clear and confirmed:

1. **HTML templates were never updated after JS refactoring**: The previous bugfix spec (`fix-frontend-api-mismatch`) updated JS to match backend APIs but did not update HTML templates to match the new JS DOM ID expectations. The HTML still uses the original IDs from the initial scaffold.

2. **Missing form markup in certificates.html and machines.html**: The templates only have toolbar buttons and tables, but lack the modal/form HTML that `certificates.js` and `machines.js` try to bind via `getElementById`. The JS uses `App.showModal()` to dynamically create modals, but `setupCertificateEvents()` and `setupMachineEvents()` try to bind to forms that should exist in the initial page load.

3. **Install command design gap**: The `GET /api/machines/{id}/install-command` endpoint intentionally uses `<AGENT_TOKEN>` placeholder (to avoid write side-effects on GET). But the frontend doesn't assemble the complete command when it already has the token (from create/regenerate responses).

4. **Field name typo in certificates.js**: `showManualDNSChallenges()` reads `ch.record_name` and `ch.record_value` but the `ManualDNSChallenge` struct serializes as `txt_record_name` and `txt_record_value`.

5. **Overly strict validation in thirdpart-dns.js**: The `dns-main-domains` input has `required` attribute and `submitDNS()` checks `!mainDomainsStr` before submitting, blocking the legitimate "empty = fetch all" use case.

## Correctness Properties

Property 1: Bug Condition - HTML/JS DOM ID Alignment

_For any_ page load where JavaScript code calls `getElementById(expectedId)`, the HTML template SHALL contain an element with that exact `id` attribute, ensuring the call returns a non-null element and form binding/rendering succeeds.

**Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8**

Property 2: Bug Condition - Install Command Completeness

_For any_ machine creation or token regeneration where the frontend receives an `agent_token`, the displayed install command SHALL contain the server external URL, machine ID, and the actual token (no placeholders), forming a copy-ready executable command.

**Validates: Requirements 2.9, 2.10**

Property 3: Bug Condition - DNS Challenge Field Names

_For any_ manual DNS challenge response where the backend returns `txt_record_name` and `txt_record_value` fields, the frontend SHALL read those exact field names and display non-empty values to the user.

**Validates: Requirements 2.13**

Property 4: Bug Condition - Empty main_domains Submission

_For any_ Cloudflare DNS config creation where the user leaves main_domains empty, the frontend SHALL allow form submission and send `main_domains: []` to the backend.

**Validates: Requirements 2.14**

Property 5: Preservation - API Call Correctness

_For any_ user action that triggers an API call, the fixed HTML/JS SHALL produce exactly the same HTTP request (URL, method, headers, body) as the current JS code intends, preserving all existing API interaction logic.

**Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10, 3.11, 3.12**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `web/templates/init.html`

**Changes**:
1. **Rename section IDs**: `id="step-admin"` → `id="init-admin-section"`, `id="step-config"` → `id="init-config-section"`
2. **Rename form IDs**: `id="admin-form"` → `id="init-admin-form"`, `id="config-form"` → `id="init-config-form"`
3. **Rename input IDs**: `id="username"` → `id="admin-username"`, `id="password"` → `id="admin-password"`
4. **Add all config fields**: Add inputs for `cfg-external-url`, `cfg-listen-addr`, `cfg-heartbeat-timeout`, `cfg-poll-interval`, `cfg-alert-days`, `cfg-certbot-path`, `cfg-certbot-datadir`, `cfg-certbot-email`, `cfg-readonly-enabled`, `cfg-readonly-password`, `cfg-monitor-port`, `cfg-monitor-interval`
5. **Remove fields not needed by JS**: Remove `password-confirm`, `domain-check-interval`, `cert-renew-days` (JS doesn't read these)
6. **Keep existing `readonly-password` and `certbot-email`** but change their IDs to `cfg-readonly-password` and `cfg-certbot-email`

---

**File**: `web/templates/login.html`

**Changes**:
1. **Rename input IDs**: `id="login-username"` → `id="username"`, `id="login-password"` → `id="password"`
2. **Rename readonly form ID**: `id="readonly-form"` → `id="readonly-login-form"`
3. **Rename readonly password ID**: `id="readonly-pwd"` → `id="readonly-password"`
4. **Update `for` attributes** on labels to match new IDs

---

**File**: `web/templates/certificates.html`

**Changes**:
1. **Rename tbody ID**: `id="certs-body"` → `id="certificates-tbody"`
2. **Add upload certificate form** (hidden by default, shown via button): form with `id="upload-cert-form"` containing inputs `cert-name`, `cert-file`, `key-file`, `chain-file`, `cert-auto-renew`
3. **Add Cloudflare issue form** (hidden by default): form with `id="issue-cloudflare-form"` containing inputs `cf-cert-name`, `cf-domains`, `cf-dns-id`, `cf-auto-renew`
4. **Add manual DNS form** (hidden by default): form with `id="manual-dns-form"` containing inputs `mdns-cert-name`, `mdns-domains`, `mdns-email`
5. **Update toolbar buttons** to show/hide the appropriate forms
6. **Update table headers** to match what `renderCertificateList()` renders (name, domains, source, status, auto_renew, created_at, actions)

---

**File**: `web/templates/machines.html`

**Changes**:
1. **Rename tbody ID**: `id="machines-body"` → `id="machines-tbody"`
2. **Add create machine form** (hidden by default): form with `id="create-machine-form"` containing inputs `machine-name`, `machine-ip`, `machine-tags`, `machine-remark`
3. **Add filter form**: form with `id="machine-filter-form"` containing inputs `filter-status`, `filter-search`
4. **Update toolbar button** to show the create form
5. **Update table headers** to match what `renderMachineList()` renders (name, ip, status, tags, agent_version, last_heartbeat_at, actions = 7 columns)

---

**File**: `web/static/js/machines.js`

**Function**: `createMachine()`

**Changes**:
1. **Enhance install command display**: After successful `POST /api/machines`, use the returned `data.machine.id` + `data.agent_token` + known server URL to assemble and display a complete install command
2. **Enhance `regenerateToken()`**: After successful token regeneration, also display the complete install command using the machine ID from context and the new token

---

**File**: `web/static/js/certificates.js`

**Function**: `showManualDNSChallenges()`

**Changes**:
1. **Fix field names**: Change `ch.record_name` → `ch.txt_record_name`, `ch.record_value` → `ch.txt_record_value`

---

**File**: `web/static/js/thirdpart-dns.js`

**Function**: `showAddModal()` and `submitDNS()`

**Changes**:
1. **Remove `required` attribute** from `dns-main-domains` input
2. **Remove non-empty validation** in `submitDNS()`: change `if (!name || !mainDomainsStr)` to `if (!name)`
3. **Handle empty mainDomainsStr**: When empty, send `main_domains: []`

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write Go tests that read the HTML template files and JS files, then verify DOM ID consistency using string matching. Run these tests on the UNFIXED code to observe failures.

**Test Cases**:
1. **Init Page ID Test**: Verify `init.html` contains all IDs that `init.js` references via `getElementById` (will fail on unfixed code)
2. **Login Page ID Test**: Verify `login.html` contains all IDs that `login.js` references via `getElementById` (will fail on unfixed code)
3. **Certificates Page ID Test**: Verify `certificates.html` contains `certificates-tbody` and form IDs (will fail on unfixed code)
4. **Machines Page ID Test**: Verify `machines.html` contains `machines-tbody` and form IDs (will fail on unfixed code)
5. **DNS Field Name Test**: Verify `certificates.js` reads `txt_record_name`/`txt_record_value` (will fail on unfixed code)
6. **Main Domains Validation Test**: Verify `thirdpart-dns.js` does NOT have `required` on main_domains and allows empty submission (will fail on unfixed code)

**Expected Counterexamples**:
- `init.html` contains `id="step-admin"` but `init.js` expects `init-admin-section`
- `login.html` contains `id="login-username"` but `login.js` expects `username`
- `certificates.html` contains `id="certs-body"` but `certificates.js` expects `certificates-tbody`
- `certificates.js` reads `ch.record_name` but backend returns `txt_record_name`

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed templates/JS produce the expected behavior.

**Pseudocode:**
```
FOR ALL page IN [init, login, certificates, machines] DO
  js_ids := extractGetElementByIdCalls(page.js_file)
  html_ids := extractIdAttributes(page.html_file)
  FOR ALL id IN js_ids DO
    ASSERT id IN html_ids
  END FOR
END FOR

FOR ALL challenge IN manual_dns_response.challenges DO
  ASSERT certificates.js reads challenge.txt_record_name (not record_name)
  ASSERT certificates.js reads challenge.txt_record_value (not record_value)
END FOR

FOR ALL dns_config_submission WHERE main_domains is empty DO
  ASSERT thirdpart-dns.js allows submission
  ASSERT request body contains main_domains: []
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed code produces the same result as the original.

**Pseudocode:**
```
FOR ALL api_call IN js_files DO
  ASSERT api_call.url is unchanged
  ASSERT api_call.method is unchanged
  ASSERT api_call.body_fields are unchanged
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It can generate many combinations of page states and verify API calls remain correct
- It catches edge cases where template changes might accidentally break existing JS logic
- It provides strong guarantees that the fix is minimal and targeted

**Test Plan**: Read the FIXED JS files and verify all API endpoint URLs, HTTP methods, and request body field names match the backend handler expectations.

**Test Cases**:
1. **API URL Preservation**: Verify all `App.get()`, `App.post()`, `App.put()`, `App.delete()` calls use correct URLs
2. **Request Body Preservation**: Verify field names in request bodies match backend struct tags
3. **Response Field Preservation**: Verify JS reads correct field names from API responses
4. **Auth Flow Preservation**: Verify login/init flows still call correct endpoints with correct bodies

### Unit Tests

- Test that each HTML template contains all DOM IDs referenced by its corresponding JS file
- Test that `certificates.js` reads `txt_record_name` and `txt_record_value` (not `record_name`/`record_value`)
- Test that `thirdpart-dns.js` does not have `required` on main_domains input
- Test that `machines.js` `createMachine()` displays a complete install command with server URL, machine ID, and token
- Test that `machines.js` `regenerateToken()` displays a complete install command

### Property-Based Tests

- Generate random sets of DOM IDs and verify JS-HTML consistency checking logic works
- Generate random API response payloads and verify field name extraction is correct
- Generate random machine creation responses and verify install command assembly includes all required parts

### Integration Tests

- Test full init flow: load page → create admin → save config → redirect to login
- Test full login flow: load page → enter credentials → submit → receive token
- Test certificate upload flow: load page → click upload → fill form → submit
- Test machine creation flow: load page → click add → fill form → submit → see install command
- Test manual DNS flow: start challenge → see TXT record values → complete
- Test Cloudflare DNS config with empty main_domains: submit → success
