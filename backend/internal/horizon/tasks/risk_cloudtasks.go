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

type RiskCloudTasksDispatcher struct {
	client *cloudtasks.Client

	projectID string
	location  string
	queue     string

	targetURL          string
	oidcServiceAccount string
	oidcAudience       string
}

type RiskTaskPayload struct {
	JobID string `json:"jobId"`
}

func NewRiskCloudTasksDispatcher(ctx context.Context, projectID, location, queue, targetURL, oidcServiceAccount, oidcAudience string) (*RiskCloudTasksDispatcher, error) {
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
	return &RiskCloudTasksDispatcher{
		client:             c,
		projectID:          strings.TrimSpace(projectID),
		location:           strings.TrimSpace(location),
		queue:              strings.TrimSpace(queue),
		targetURL:          strings.TrimSpace(targetURL),
		oidcServiceAccount: strings.TrimSpace(oidcServiceAccount),
		oidcAudience:       strings.TrimSpace(oidcAudience),
	}, nil
}

func (d *RiskCloudTasksDispatcher) Close() error {
	if d == nil || d.client == nil {
		return nil
	}
	return d.client.Close()
}

func (d *RiskCloudTasksDispatcher) EnqueueRiskJob(ctx context.Context, jobID string) error {
	if d == nil || d.client == nil {
		return fmt.Errorf("cloud tasks dispatcher not initialized")
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return fmt.Errorf("jobID is required")
	}

	apiCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s/queues/%s", d.projectID, d.location, d.queue)
	payload := RiskTaskPayload{JobID: jobID}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal risk task payload: %w", err)
	}

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

	_, err = d.client.CreateTask(apiCtx, req)
	return err
}
