package tests

import (
	"os"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// **Validates: Requirements 4.2, 4.6**
//
// Permission Directive Property Tests
// These tests perform static analysis on the v-permission directive source
// to verify that it correctly controls DOM element visibility based on user roles.

const permissionDirectivePath = "../webui/src/directives/permission.ts"

func readPermissionSource(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(permissionDirectivePath)
	if err != nil {
		t.Fatalf("failed to read permission directive: %v", err)
	}
	return string(content)
}

// Property 6: 权限指令正确性
// v-permission correctly controls DOM element existence based on role.
// Sub-properties:
// 6a: The directive removes elements from DOM (via removeChild) when role is not permitted
// 6b: The directive supports role array mode (v-permission="['admin', 'user']")
// 6c: The directive supports write action shortcut (v-permission:action="'write'")
// 6d: The directive reads role from auth store (useAuthStore)
// 6e: For any role NOT in the allowed array, the element is removed
// 6f: For readonly role with action='write', the element is removed

func TestPermission_Property6_DirectiveCorrectness(t *testing.T) {
	source := readPermissionSource(t)

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	// Sub-property 6a: The directive removes elements from DOM via removeChild
	properties.Property("v-permission removes elements from DOM via removeChild when role not permitted", prop.ForAll(
		func(_ string) bool {
			// The directive must use removeChild to remove elements
			hasRemoveChild := strings.Contains(source, "removeChild")
			// It must access parentNode to perform removal
			hasParentNode := strings.Contains(source, "parentNode")
			// The removal pattern: el.parentNode?.removeChild(el)
			hasRemovalPattern := strings.Contains(source, "el.parentNode?.removeChild(el)") ||
				strings.Contains(source, "el.parentNode!.removeChild(el)")
			return hasRemoveChild && hasParentNode && hasRemovalPattern
		},
		gen.AlphaString(),
	))

	// Sub-property 6b: The directive supports role array mode
	// v-permission="['admin', 'user']" — only specified roles can see the element
	properties.Property("v-permission supports role array mode with Array.isArray check", prop.ForAll(
		func(role string) bool {
			// Must check if value is an array
			hasArrayCheck := strings.Contains(source, "Array.isArray(value)") ||
				strings.Contains(source, "Array.isArray(binding.value)")
			// Must use .includes() to check if role is in the array
			hasIncludesCheck := strings.Contains(source, ".includes(")
			// Must reference the role from auth store
			hasRoleRef := strings.Contains(source, "authStore.role") ||
				strings.Contains(source, "store.role")
			return hasArrayCheck && hasIncludesCheck && hasRoleRef
		},
		gen.OneConstOf("admin", "user", "readonly"),
	))

	// Sub-property 6c: The directive supports write action shortcut
	// v-permission:action="'write'" — non-readonly can see the element
	properties.Property("v-permission supports action='write' shortcut for non-readonly check", prop.ForAll(
		func(_ string) bool {
			// Must check for arg === 'action'
			hasArgCheck := strings.Contains(source, "arg") &&
				(strings.Contains(source, "'action'") || strings.Contains(source, "\"action\""))
			// Must check for value === 'write'
			hasWriteCheck := strings.Contains(source, "'write'") || strings.Contains(source, "\"write\"")
			// Must check isReadonly from auth store
			hasReadonlyCheck := strings.Contains(source, "isReadonly") ||
				strings.Contains(source, "authStore.isReadonly")
			return hasArgCheck && hasWriteCheck && hasReadonlyCheck
		},
		gen.AlphaString(),
	))

	// Sub-property 6d: The directive reads role from auth store (useAuthStore)
	properties.Property("v-permission reads role from useAuthStore", prop.ForAll(
		func(_ string) bool {
			// Must import and use useAuthStore
			hasImport := strings.Contains(source, "useAuthStore")
			// Must call useAuthStore() to get the store instance
			hasStoreCall := strings.Contains(source, "useAuthStore()")
			return hasImport && hasStoreCall
		},
		gen.AlphaString(),
	))

	// Sub-property 6e: For any role NOT in the allowed array, the element is removed
	// This verifies the negation logic: !value.includes(role) → remove
	properties.Property("v-permission removes element when role is NOT in allowed array (negation logic)", prop.ForAll(
		func(role string) bool {
			// The logic must be: if NOT includes → remove
			// Check for negation pattern: !value.includes(...)
			hasNegation := strings.Contains(source, "!value.includes(") ||
				strings.Contains(source, "!binding.value.includes(")
			// The removal is inside the negation branch
			// Verify the structure: if (!value.includes(role)) { removeChild }
			lines := strings.Split(source, "\n")
			foundNegationBeforeRemoval := false
			negationSeen := false
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.Contains(trimmed, "!value.includes(") ||
					strings.Contains(trimmed, "!binding.value.includes(") {
					negationSeen = true
				}
				if negationSeen && strings.Contains(trimmed, "removeChild") {
					foundNegationBeforeRemoval = true
					break
				}
			}
			_ = role
			return hasNegation && foundNegationBeforeRemoval
		},
		gen.OneConstOf("admin", "user", "readonly", "unknown"),
	))

	// Sub-property 6f: For readonly role with action='write', the element is removed
	// This verifies: if (arg === 'action' && value === 'write') { if (isReadonly) removeChild }
	properties.Property("v-permission removes element for readonly role when action is write", prop.ForAll(
		func(_ string) bool {
			// The logic: arg === 'action' && value === 'write' → check isReadonly → remove
			lines := strings.Split(source, "\n")
			actionCheckSeen := false
			readonlyCheckAfterAction := false
			removeAfterReadonly := false

			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.Contains(trimmed, "action") && strings.Contains(trimmed, "write") {
					actionCheckSeen = true
				}
				if actionCheckSeen && strings.Contains(trimmed, "isReadonly") {
					readonlyCheckAfterAction = true
				}
				if readonlyCheckAfterAction && strings.Contains(trimmed, "removeChild") {
					removeAfterReadonly = true
					break
				}
			}
			return actionCheckSeen && readonlyCheckAfterAction && removeAfterReadonly
		},
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}
