package ai

import (
	"context"
	"strings"
)

type usageCtxKey string

const (
	usageTenantKey  usageCtxKey = "ai.tenantId"
	usageUserKey    usageCtxKey = "ai.userId"
	usageProjectKey usageCtxKey = "ai.projectId"
	usageUseCaseKey usageCtxKey = "ai.useCase"

	usageHorizonLensKey    usageCtxKey = "ai.horizon.lensId"
	usageHorizonCascadeKey usageCtxKey = "ai.horizon.cascadeId"
)

// WithUsageContext injects usage/telemetry metadata into context so callers don't need to
// plumb TenantID/UserID into every AITask object.
func WithUsageContext(ctx context.Context, tenantID, userID, projectID, useCase, lensID, cascadeID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(tenantID) != "" {
		ctx = context.WithValue(ctx, usageTenantKey, strings.TrimSpace(tenantID))
	}
	if strings.TrimSpace(userID) != "" {
		ctx = context.WithValue(ctx, usageUserKey, strings.TrimSpace(userID))
	}
	if strings.TrimSpace(projectID) != "" {
		ctx = context.WithValue(ctx, usageProjectKey, strings.TrimSpace(projectID))
	}
	if strings.TrimSpace(useCase) != "" {
		ctx = context.WithValue(ctx, usageUseCaseKey, strings.TrimSpace(useCase))
	}
	if strings.TrimSpace(lensID) != "" {
		ctx = context.WithValue(ctx, usageHorizonLensKey, strings.TrimSpace(lensID))
	}
	if strings.TrimSpace(cascadeID) != "" {
		ctx = context.WithValue(ctx, usageHorizonCascadeKey, strings.TrimSpace(cascadeID))
	}
	return ctx
}

func usageTenantFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(usageTenantKey).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func usageUserFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(usageUserKey).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func usageProjectFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(usageProjectKey).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func usageUseCaseFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(usageUseCaseKey).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func usageLensFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(usageHorizonLensKey).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func usageCascadeFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(usageHorizonCascadeKey).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
