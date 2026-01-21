package tasks

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

type ProfileCloudTasksDispatcher struct {
	client *cloudtasks.Client

	projectID string
	location  string
	queue     string

	targetURL          string
	oidcServiceAccount string
	oidcAudience       string
}

type ProfileTaskPayload struct {
	FragmentID string `json:"fragmentId"`
	Force      bool   `json:"force,omitempty"`
}

func NewProfileCloudTasksDispatcher(ctx context.Context, projectID, location, queue, targetURL, oidcServiceAccount, oidcAudience string) (*ProfileCloudTasksDispatcher, error) {
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

	c, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return &ProfileCloudTasksDispatcher{
		client:             c,
		projectID:          strings.TrimSpace(projectID),
		location:           strings.TrimSpace(location),
		queue:              strings.TrimSpace(queue),
		targetURL:          strings.TrimSpace(targetURL),
		oidcServiceAccount: strings.TrimSpace(oidcServiceAccount),
		oidcAudience:       strings.TrimSpace(oidcAudience),
	}, nil
}

func (d *ProfileCloudTasksDispatcher) Close() error {
	if d == nil || d.client == nil {
		return nil
	}
	return d.client.Close()
}

func (d *ProfileCloudTasksDispatcher) EnqueueProfileRefresh(ctx context.Context, fragmentID string, force bool) error {
	if d == nil || d.client == nil {
		return fmt.Errorf("cloud tasks dispatcher not initialized")
	}
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" {
		return fmt.Errorf("fragmentID is required")
	}

	apiCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s/queues/%s", d.projectID, d.location, d.queue)
	payload := ProfileTaskPayload{FragmentID: fragmentID, Force: force}
	b, _ := json.Marshal(payload)

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
					Body: b,
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
