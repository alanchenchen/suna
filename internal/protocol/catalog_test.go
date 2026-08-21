package protocol

import (
	"slices"
	"testing"
)

func TestRuntimeCatalogIsStableAndContainsPublicCapabilities(t *testing.T) {
	catalog := CurrentRuntimeCatalog()
	assertCatalogGroup(t, "methods", catalog.Methods)
	assertCatalogGroup(t, "notifications", catalog.Notifications)
	assertCatalogGroup(t, "features", catalog.Features)

	for _, method := range []string{MethodRuntimeHello, MethodSendMessage, MethodSteer, MethodConfigGet, MethodMCPList} {
		if !slices.Contains(catalog.Methods, method) {
			t.Fatalf("catalog methods missing %q", method)
		}
	}
	for _, notification := range []string{NotifyAgentDelta, NotifyAgentRun, NotifySteering, NotifyConfigState, NotifyMCPUpdated} {
		if !slices.Contains(catalog.Notifications, notification) {
			t.Fatalf("catalog notifications missing %q", notification)
		}
	}
	for _, feature := range []string{FeatureRunSteeringText, FeatureModelAuthModeBearer, FeatureModelAuthModeBoth, FeatureProjectSkills} {
		if !slices.Contains(catalog.Features, feature) {
			t.Fatalf("catalog features missing %q", feature)
		}
	}
	for _, internal := range []string{MethodDaemonStop, MethodDebugMemory, MethodAttachmentClear, MethodTriggerList} {
		if slices.Contains(catalog.Methods, internal) {
			t.Fatalf("catalog methods unexpectedly expose internal or reserved method %q", internal)
		}
	}
	if slices.Contains(catalog.Notifications, NotifyDaemonFullStatus) || slices.Contains(catalog.Notifications, NotifyPerception) {
		t.Fatalf("catalog notifications expose internal or reserved notification: %#v", catalog.Notifications)
	}
}

func TestRuntimeCatalogReturnsIndependentSlices(t *testing.T) {
	first := CurrentRuntimeCatalog()
	first.Methods[0] = "changed"
	first.Notifications[0] = "changed"
	first.Features[0] = "changed"
	second := CurrentRuntimeCatalog()
	if second.Methods[0] == "changed" || second.Notifications[0] == "changed" || second.Features[0] == "changed" {
		t.Fatalf("CurrentRuntimeCatalog() returned shared slices: %#v", second)
	}
}

func assertCatalogGroup(t *testing.T, name string, values []string) {
	t.Helper()
	if len(values) == 0 {
		t.Fatalf("catalog %s is empty", name)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			t.Fatalf("catalog %s contains an empty item", name)
		}
		if _, ok := seen[value]; ok {
			t.Fatalf("catalog %s contains duplicate %q", name, value)
		}
		seen[value] = struct{}{}
	}
	if !slices.IsSorted(values) {
		t.Fatalf("catalog %s is not sorted: %#v", name, values)
	}
}
