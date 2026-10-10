package management

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// FingerprintOwnershipSnapshot returns the stable SHA-256 fingerprint for one
// resolved target. It includes the target, the observed ZLM presence and every
// ownership fact, so a change in either the resource or its business holder
// requires a new confirmation.
func FingerprintOwnershipSnapshot(snapshot OwnershipSnapshot) string {
	return FingerprintOwnershipSnapshots([]OwnershipSnapshot{snapshot})
}

// FingerprintOwnershipSnapshots hashes a canonical, sorted representation of
// resolved targets. The input order is deliberately ignored; duplicates are
// retained as distinct observations so a collection change cannot be hidden by
// normalisation.
func FingerprintOwnershipSnapshots(snapshots []OwnershipSnapshot) string {
	canonical := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		canonical = append(canonical, canonicalOwnershipSnapshot(snapshot))
	}
	sort.Strings(canonical)
	return fingerprintParts(canonical...)
}

// FingerprintOwnershipTargets hashes a target collection without requiring a
// live ownership read. It is useful when a controller needs to bind an
// explicit target set before asking the resolver for impacts.
func FingerprintOwnershipTargets(targets []OwnershipTarget) string {
	canonical := make([]string, 0, len(targets))
	for _, target := range targets {
		canonical = append(canonical, canonicalOwnershipTarget(target))
	}
	sort.Strings(canonical)
	return fingerprintParts(canonical...)
}

// FingerprintImpacts returns a stable digest for safe conflict summaries. The
// media tuple is included when present; impact order is not significant.
func FingerprintImpacts(impacts []Impact) string {
	canonical := make([]string, 0, len(impacts))
	for _, impact := range impacts {
		media := MediaIdentity{}
		if impact.MediaIdentity != nil {
			media = *impact.MediaIdentity
		}
		canonical = append(canonical, canonicalParts(
			impact.ResourceType, impact.ResourceKey, impact.Owner, impact.Reason,
			media.Schema, media.Vhost, media.App, media.Stream,
		))
	}
	sort.Strings(canonical)
	return fingerprintParts(canonical...)
}

func canonicalOwnershipSnapshot(snapshot OwnershipSnapshot) string {
	owners := make([]string, 0, len(snapshot.Owners))
	for _, owner := range snapshot.Owners {
		owners = append(owners, canonicalOwnershipEvidence(owner))
	}
	sort.Strings(owners)
	return canonicalParts(
		canonicalOwnershipTarget(snapshot.Target),
		string(snapshot.Status), strconv.FormatBool(snapshot.Present), strconv.FormatBool(snapshot.PresenceKnown),
		strings.Join(owners, ""),
	)
}

func canonicalOwnershipTarget(target OwnershipTarget) string {
	return canonicalParts(
		strconv.FormatInt(target.NodeID, 10), target.Media.Schema, target.Media.Vhost,
		target.Media.App, target.Media.Stream,
	)
}

func canonicalParts(parts ...string) string {
	var builder strings.Builder
	for _, part := range parts {
		builder.WriteString(strconv.Itoa(len(part)))
		builder.WriteByte(':')
		builder.WriteString(part)
		builder.WriteByte('|')
	}
	return builder.String()
}

func fingerprintParts(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = fmt.Fprintf(hash, "%d:", len(part))
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{'|'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
