package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthMergeRejectsAmbiguousV27(t *testing.T) {
	svc := &APIKeyService{}
	// Both the deployed fork and upstream used27 for incompatible field sets.
	entry := &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{
		Version: 27, APIKeyID: 1, UserID: 2, User: APIKeyAuthUserSnapshot{ID: 2},
		Group: &APIKeyAuthGroupSnapshot{ID: 3},
	}}
	key, used, err := svc.applyAuthCacheEntry("sk-merge-test", entry)
	require.NoError(t, err)
	require.False(t, used, "ambiguous v27 must miss the cache so auth reloads complete fields")
	require.Nil(t, key)
}

func TestAPIKeyAuthMergeRoundtripPreservesMILEAndPrivacy(t *testing.T) {
	svc := &APIKeyService{}
	groupID := int64(3)
	key := &APIKey{ID: 1, UserID: 2, GroupID: &groupID, User: &User{ID: 2},
		Group: &Group{ID: 3, UsageKind: UsageKindCoding, ImageCountBilling: true, RequirePrivacySet: true}}
	bytes, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), key)})
	require.NoError(t, err)
	var entry APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(bytes, &entry))
	restored, used, err := svc.applyAuthCacheEntry("sk-merge-test", &entry)
	require.NoError(t, err)
	require.True(t, used)
	require.Equal(t, UsageKindCoding, restored.Group.UsageKind)
	require.True(t, restored.Group.ImageCountBilling)
	require.True(t, restored.Group.RequirePrivacySet)
}
