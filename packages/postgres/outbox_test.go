package postgres

import (
	"strings"
	"testing"
)

func TestOutboxClaimQueryRequiresAggregateOrderingAndDatabaseClaim(t *testing.T) {
	for _, fragment := range []string{
		"e.sent_at IS NULL",
		"prior.aggregate_id = e.aggregate_id",
		"prior.aggregate_version < e.aggregate_version",
		"prior.sent_at IS NULL",
		"ORDER BY e.aggregate_id, e.aggregate_version, e.id",
		"FOR UPDATE SKIP LOCKED",
	} {
		if !strings.Contains(outboxClaimQuery, fragment) {
			t.Fatalf("outbox claim query missing %q:\n%s", fragment, outboxClaimQuery)
		}
	}
}

func TestOutboxOrderingContractRequiresMonotonicAggregateVersion(t *testing.T) {
	for _, fragment := range []string{
		"aggregate_id text NOT NULL",
		"aggregate_version bigint NOT NULL",
		"UNIQUE (aggregate_id, aggregate_version)",
	} {
		if !strings.Contains(outboxOrderingContract, fragment) {
			t.Fatalf("outbox ordering contract missing %q:\n%s", fragment, outboxOrderingContract)
		}
	}
}
