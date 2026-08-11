package cache

import "testing"

func TestEmptyInitializesCollectionMaps(t *testing.T) {
	snapshot := Empty("https://example.okta.com")
	if snapshot.OrgURL != "https://example.okta.com" {
		t.Fatalf("OrgURL = %q", snapshot.OrgURL)
	}

	snapshot.AppUsers["app"] = nil
	snapshot.AppGroups["app"] = nil
	snapshot.GroupMembers["group"] = nil
}
