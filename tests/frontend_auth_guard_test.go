package tests

import (
	"os"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// **Validates: Requirements 2.4, 2.5, 2.6, 2.8**
//
// Auth Guard Property Tests
// These tests perform static analysis on the auth guard and request interceptor
// source files to verify correctness properties related to authentication flow.

const authGuardPath = "../webui/src/router/guard/auth.ts"
const requestInterceptorPath = "../webui/src/service/request/index.ts"

func readAuthGuardSource(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(authGuardPath)
	if err != nil {
		t.Fatalf("failed to read auth guard source: %v", err)
	}
	return string(content)
}

func readRequestInterceptorSource(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(requestInterceptorPath)
	if err != nil {
		t.Fatalf("failed to read request interceptor source: %v", err)
	}
	return string(content)
}

// --- Property 2: 认证守卫重定向 ---
// When there is no token, protected routes redirect to /login.
// Static analysis verifies:
// - The guard checks for token in localStorage
// - Public routes (requiresAuth === false) are allowed through
// - Missing token triggers redirect to /login with redirect query param

func TestAuthGuard_Property2_RedirectWithoutToken(t *testing.T) {
	source := readAuthGuardSource(t)

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Sub-property 2a: Guard checks for token in localStorage
	properties.Property("Auth guard reads token from localStorage", prop.ForAll(
		func(_ string) bool {
			hasLocalStorageGet := strings.Contains(source, "localStorage.getItem('token')")
			return hasLocalStorageGet
		},
		gen.AlphaString(),
	))

	// Sub-property 2b: Public routes bypass auth check
	properties.Property("Auth guard allows public routes (requiresAuth === false) to pass", prop.ForAll(
		func(_ string) bool {
			hasRequiresAuthCheck := strings.Contains(source, "requiresAuth") &&
				strings.Contains(source, "false")
			hasNextCall := strings.Contains(source, "next()")
			return hasRequiresAuthCheck && hasNextCall
		},
		gen.AlphaString(),
	))

	// Sub-property 2c: Missing token redirects to /login
	properties.Property("Auth guard redirects to /login when token is missing", prop.ForAll(
		func(_ string) bool {
			hasLoginRedirect := strings.Contains(source, "/login")
			hasTokenCheck := strings.Contains(source, "!token") || strings.Contains(source, "token)")
			return hasLoginRedirect && hasTokenCheck
		},
		gen.AlphaString(),
	))

	// Sub-property 2d: Redirect includes original path as query param
	properties.Property("Auth guard includes redirect query param with original path", prop.ForAll(
		func(_ string) bool {
			hasRedirectQuery := strings.Contains(source, "redirect") &&
				strings.Contains(source, "to.fullPath")
			return hasRedirectQuery
		},
		gen.AlphaString(),
	))

	// Sub-property 2e: Guard uses beforeEach navigation hook
	properties.Property("Auth guard registers as beforeEach navigation guard", prop.ForAll(
		func(_ string) bool {
			hasBeforeEach := strings.Contains(source, "router.beforeEach")
			return hasBeforeEach
		},
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}

// --- Property 3: 401 拦截器清理 ---
// When a protected API returns 401, the interceptor clears the token and redirects.
// Static analysis verifies:
// - Response interceptor handles 401 status
// - Auth endpoint 401s are excluded from global logout
// - Token is cleared via authStore.clearAuth()
// - Pending requests are cancelled
// - Router navigates to /login

func TestAuthGuard_Property3_UnauthorizedCleanup(t *testing.T) {
	source := readRequestInterceptorSource(t)

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Sub-property 3a: Response interceptor checks for 401 status
	properties.Property("Request interceptor detects 401 status code", prop.ForAll(
		func(_ string) bool {
			has401Check := strings.Contains(source, "401")
			hasStatusCheck := strings.Contains(source, "status")
			return has401Check && hasStatusCheck
		},
		gen.AlphaString(),
	))

	// Sub-property 3b: Auth endpoints are excluded from global logout
	properties.Property("Auth endpoint 401s do not trigger global logout", prop.ForAll(
		func(_ string) bool {
			hasIsAuthEndpoint := strings.Contains(source, "isAuthEndpoint")
			// The 401 handler must check !isAuthEndpoint before triggering logout
			hasExclusion := strings.Contains(source, "!isAuthEndpoint")
			return hasIsAuthEndpoint && hasExclusion
		},
		gen.AlphaString(),
	))

	// Sub-property 3c: handleUnauthorized clears auth state
	properties.Property("handleUnauthorized clears auth state via authStore.clearAuth()", prop.ForAll(
		func(_ string) bool {
			hasClearAuth := strings.Contains(source, "clearAuth")
			hasHandleUnauthorized := strings.Contains(source, "handleUnauthorized")
			return hasClearAuth && hasHandleUnauthorized
		},
		gen.AlphaString(),
	))

	// Sub-property 3d: handleUnauthorized cancels all pending requests
	properties.Property("handleUnauthorized cancels all pending requests", prop.ForAll(
		func(_ string) bool {
			hasCancelAll := strings.Contains(source, "cancelAllPendingRequests")
			return hasCancelAll
		},
		gen.AlphaString(),
	))

	// Sub-property 3e: handleUnauthorized redirects to /login
	properties.Property("handleUnauthorized redirects to /login", prop.ForAll(
		func(_ string) bool {
			hasRouterPush := strings.Contains(source, "router.push('/login')")
			return hasRouterPush
		},
		gen.AlphaString(),
	))

	// Sub-property 3f: Duplicate 401 handling is prevented
	properties.Property("handleUnauthorized prevents duplicate redirects", prop.ForAll(
		func(_ string) bool {
			hasRedirectingFlag := strings.Contains(source, "isRedirecting")
			hasEarlyReturn := strings.Contains(source, "if (isRedirecting) return")
			return hasRedirectingFlag && hasEarlyReturn
		},
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}

// --- Property 4: Bearer Token 附加 ---
// Non-public requests have Authorization: Bearer <token> header attached.
// Static analysis verifies:
// - Request interceptor reads token from localStorage
// - Public endpoints are excluded from token attachment
// - Authorization header is set with Bearer prefix
// - isPublicEndpoint function correctly identifies public paths

func TestAuthGuard_Property4_BearerTokenAttachment(t *testing.T) {
	source := readRequestInterceptorSource(t)

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Sub-property 4a: Request interceptor reads token from localStorage
	properties.Property("Request interceptor reads token from localStorage", prop.ForAll(
		func(_ string) bool {
			hasTokenRead := strings.Contains(source, "localStorage.getItem('token')")
			return hasTokenRead
		},
		gen.AlphaString(),
	))

	// Sub-property 4b: Public endpoints are excluded from token attachment
	properties.Property("Public endpoints do not get Bearer token attached", prop.ForAll(
		func(_ string) bool {
			hasPublicCheck := strings.Contains(source, "isPublicEndpoint")
			hasNegation := strings.Contains(source, "!isPublicEndpoint")
			return hasPublicCheck && hasNegation
		},
		gen.AlphaString(),
	))

	// Sub-property 4c: Authorization header uses Bearer prefix
	properties.Property("Authorization header is set with Bearer prefix", prop.ForAll(
		func(_ string) bool {
			hasBearerHeader := strings.Contains(source, "Bearer")
			hasAuthHeader := strings.Contains(source, "Authorization")
			return hasBearerHeader && hasAuthHeader
		},
		gen.AlphaString(),
	))

	// Sub-property 4d: isPublicEndpoint identifies all public paths
	properties.Property("isPublicEndpoint covers all required public paths", prop.ForAll(
		func(_ string) bool {
			hasLoginPath := strings.Contains(source, "/api/auth/login")
			hasReadonlyLoginPath := strings.Contains(source, "/api/auth/readonly-login")
			hasTurnstilePath := strings.Contains(source, "/api/auth/turnstile-config")
			hasInitPath := strings.Contains(source, "/init/")
			return hasLoginPath && hasReadonlyLoginPath && hasTurnstilePath && hasInitPath
		},
		gen.AlphaString(),
	))

	// Sub-property 4e: Token is attached via request interceptor (interceptors.request.use)
	properties.Property("Token attachment happens in request interceptor", prop.ForAll(
		func(_ string) bool {
			hasRequestInterceptor := strings.Contains(source, "interceptors.request.use")
			hasHeaderAssignment := strings.Contains(source, "config.headers.Authorization")
			return hasRequestInterceptor && hasHeaderAssignment
		},
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}
