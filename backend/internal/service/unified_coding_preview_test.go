//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUnifiedCodingPreviewAccess(t *testing.T) {
	group := &Group{ID: 50, Platform: PlatformComposite, UsageKind: UsageKindCoding, IsExclusive: true}
	for _, tc := range []struct {
		name    string
		user    User
		allowed bool
	}{
		{"admin", User{ID: 1, Role: RoleAdmin}, true},
		{"uninvited", User{ID: 2, Role: RoleUser}, false},
		{"invited", User{ID: 3, Role: RoleUser, AllowedGroups: []int64{50}}, true},
		{"other invitation", User{ID: 4, Role: RoleUser, AllowedGroups: []int64{51}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.allowed, tc.user.CanBindLabGroup(group))
			svc := &APIKeyService{}
			require.Equal(t, tc.allowed, svc.canUserBindGroup(context.Background(), &tc.user, group))
			require.Equal(t, tc.allowed, svc.canUserBindGroupInternal(&tc.user, group, nil))
		})
	}
	admin := &User{ID: 1, Role: RoleAdmin}
	legacy := &Group{ID: 51, Platform: PlatformOpenAI, UsageKind: UsageKindCoding, IsExclusive: true}
	require.False(t, admin.CanBindLabGroup(legacy), "legacy exclusive-group permissions must not change")
	group.IsExclusive = false
	require.True(t, (&User{ID: 2, Role: RoleUser}).CanBindLabGroup(group), "explicit public release")
	group.IsExclusive = true
	invited := &User{ID: 3, Role: RoleUser, AllowedGroups: []int64{50}}
	require.True(t, invited.CanBindLabGroup(group))
	invited.AllowedGroups = nil
	require.False(t, invited.CanBindLabGroup(group), "revocation must close access")
}

func TestUnifiedCodingPreviewCreationDefaultsPrivate(t *testing.T) {
	for _, tc := range []struct {
		platform, kind string
		exclusive      bool
	}{
		{PlatformComposite, UsageKindCoding, true},
		{PlatformComposite, UsageKindExperiment, false},
		{PlatformOpenAI, UsageKindCoding, false},
		{PlatformGrok, UsageKindCoding, false},
	} {
		repo := &groupRepoStubForAdmin{createID: 50}
		svc := &adminServiceImpl{groupRepo: repo}
		group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "Preview", Platform: tc.platform, UsageKind: tc.kind, RateMultiplier: 1})
		require.NoError(t, err)
		require.Equal(t, tc.exclusive, group.IsExclusive, "%s/%s", tc.platform, tc.kind)
	}
}

func TestUnifiedCodingPreviewRejectsLegacyConversionAndAllowsExplicitPublicRelease(t *testing.T) {
	group := &Group{ID: 50, Name: "Existing experiment", Platform: PlatformComposite, UsageKind: UsageKindExperiment, SubscriptionType: SubscriptionTypeStandard, Status: StatusActive, RateMultiplier: 1}
	repo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{50: group}}
	svc := &adminServiceImpl{groupRepo: repo}
	public := false
	_, err := svc.UpdateGroup(context.Background(), 50, &UpdateGroupInput{UsageKind: UsageKindCoding, IsExclusive: &public})
	require.Error(t, err, "legacy Keys and invitations must not inherit a new feature")
	require.Equal(t, UsageKindExperiment, group.UsageKind)
	require.False(t, group.IsExclusive)
	user := &User{ID: 2, Role: RoleUser, AllowedGroups: []int64{50}}
	key := &APIKey{GroupID: &group.ID, Group: group, User: user}
	require.Equal(t, PurposeExperiment, PurposeOfGroup(key.Group.Platform, key.Group.UsageKind))
	group = &Group{ID: 50, Platform: PlatformComposite, UsageKind: UsageKindCoding, IsExclusive: true, SubscriptionType: SubscriptionTypeStandard}
	repo.getByIDByID[50] = group
	published, err := svc.UpdateGroup(context.Background(), 50, &UpdateGroupInput{IsExclusive: &public})
	require.NoError(t, err)
	require.False(t, published.IsExclusive, "later explicit admin update can publish")
}

func TestUnifiedCodingPreviewVisibilityRequiresInvitation(t *testing.T) {
	groups := []Group{{ID: 50, Platform: PlatformComposite, UsageKind: UsageKindCoding, IsExclusive: true, SubscriptionType: SubscriptionTypeSubscription}, {ID: 51, Platform: PlatformComposite, UsageKind: UsageKindCoding, IsExclusive: true}}
	user := &User{ID: 2, Role: RoleUser}
	svc := &APIKeyService{userRepo: &visibilityUserRepo{user: user}, groupRepo: &visibilityGroupRepo{groups: groups}, userSubRepo: &visibilitySubRepo{subscriptions: []UserSubscription{{UserID: 2, GroupID: 50, Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}}}}
	visible, _, err := svc.GetUserGroupVisibility(context.Background(), 2)
	require.NoError(t, err)
	require.NotContains(t, visible, int64(50), "a subscription does not invite its owner into a preview")
	user.AllowedGroups = []int64{50}
	visible, _, err = svc.GetUserGroupVisibility(context.Background(), 2)
	require.NoError(t, err)
	require.Contains(t, visible, int64(50))
	user.AllowedGroups = nil
	user.Role = RoleAdmin
	visible, _, err = svc.GetUserGroupVisibility(context.Background(), 2)
	require.NoError(t, err)
	require.Contains(t, visible, int64(51), "administrators can inspect private standard coding previews")
}

func TestUnifiedCodingPreviewDuplicateStartsPrivate(t *testing.T) {
	source := &Group{ID: 50, Platform: PlatformComposite, UsageKind: UsageKindCoding, IsExclusive: false}
	copy := cloneGroupForDuplicate(source, "preview-copy")
	require.True(t, IsUnifiedCodingGroup(copy), "copy must preserve the coding wallet classification")
	require.True(t, copy.IsExclusive, "new copy starts private even if source is public")
	require.False(t, copy.IsActive())
	require.False(t, source.IsExclusive, "source remains public")
}
