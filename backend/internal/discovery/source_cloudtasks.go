package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type SourceTaskPayload struct {
	SourceID   string `json:"sourceId"`
	TenantID   string `json:"tenantId,omitempty"`
	Kind       string `json:"kind"`
	LeaseOwner string `json:"leaseOwner,omitempty"`
}

type SourceTaskDispatcher interface {
	EnqueueSourcePoll(ctx context.Context, payload SourceTaskPayload) error
}

type SourceCloudTasksDispatcher struct {
	client *cloudtasks.Client

	projectID string
	location  string
	queue     string

	targetURL          string
	oidcServiceAccount string
	oidcAudience       string
}

func NewSourceCloudTasksDispatcher(ctx context.Context, projectID, location, queue, targetURL, oidcServiceAccount, oidcAudience string) (*SourceCloudTasksDispatcher, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("projectID is required")
	}
	if strings.TrimSpace(location) == "" {
		return nil, fmt.Errorf("location is required")
	}
	if strings.TrimSpace(queue) == "" {
		return nil, fmt.Errorf("queue is required")
	}
	if strings.TrimSpace(targetURL) == "" {
		return nil, fmt.Errorf("targetURL is required")
	}
	if strings.TrimSpace(oidcServiceAccount) == "" {
		return nil, fmt.Errorf("oidcServiceAccount is required")
	}
	if strings.TrimSpace(oidcAudience) == "" {
		oidcAudience = targetAudienceFromURL(targetURL)
	}

	client, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return &SourceCloudTasksDispatcher{
		client:             client,
		projectID:          strings.TrimSpace(projectID),
		location:           strings.TrimSpace(location),
		queue:              strings.TrimSpace(queue),
		targetURL:          strings.TrimSpace(targetURL),
		oidcServiceAccount: strings.TrimSpace(oidcServiceAccount),
		oidcAudience:       strings.TrimSpace(oidcAudience),
	}, nil
}

func (d *SourceCloudTasksDispatcher) Close() error {
	if d == nil || d.client == nil {
		return nil
	}
	return d.client.Close()
}

func (d *SourceCloudTasksDispatcher) EnqueueSourcePoll(ctx context.Context, payload SourceTaskPayload) error {
	if d == nil || d.client == nil {
		return fmt.Errorf("cloud tasks dispatcher not initialized")
	}
	payload.SourceID = strings.TrimSpace(payload.SourceID)
	payload.Kind = strings.TrimSpace(payload.Kind)
	if payload.SourceID == "" || payload.Kind == "" {
		return fmt.Errorf("sourceId and kind are required")
	}

	apiCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s/queues/%s", d.projectID, d.location, d.queue)
	body, _ := json.Marshal(payload)

	req := &taskspb.CreateTaskRequest{
		Parent: parent,
		Task: &taskspb.Task{
			MessageType: &taskspb.Task_HttpRequest{
				HttpRequest: &taskspb.HttpRequest{
					HttpMethod: taskspb.HttpMethod_POST,
					Url:        d.targetURL,
					Headers: map[string]string{
						"Content-Type": "application/json",
					},
					Body: body,
					AuthorizationHeader: &taskspb.HttpRequest_OidcToken{
						OidcToken: &taskspb.OidcToken{
							ServiceAccountEmail: d.oidcServiceAccount,
							Audience:            d.oidcAudience,
						},
					},
				},
			},
			ScheduleTime: timestamppb.New(time.Now().UTC()),
		},
	}
	_, err := d.client.CreateTask(apiCtx, req)
	return err
}

func targetAudienceFromURL(targetURL string) string {
	targetURL = strings.TrimSpace(targetURL)
	if targetURL == "" {
		return ""
	}
	if idx := strings.Index(targetURL, "://"); idx >= 0 {
		if slash := strings.Index(targetURL[idx+3:], "/"); slash >= 0 {
			return targetURL[:idx+3+slash]
		}
	}
	return targetURL
}
