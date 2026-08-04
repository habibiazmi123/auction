package main

import "testing"

func TestRequiredInternalServiceCredential(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_CREDENTIAL", "")
	if _, err := requiredInternalServiceCredential(); err == nil {
		t.Fatal("expected empty credential to be rejected")
	}
	t.Setenv("INTERNAL_SERVICE_CREDENTIAL", " internal-secret ")
	credential, err := requiredInternalServiceCredential()
	if err != nil || credential != "internal-secret" {
		t.Fatalf("credential=%q error=%v", credential, err)
	}
}
