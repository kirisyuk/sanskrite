package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type TaskPayload struct {
	TaskID string `json:"taskId"`
}

type Dispatcher struct {
	client *cloudtasks.Client

	projectID string
	location  string
	queue     string
	targetURL string
	saEmail   string
	audience  string
}

func NewDispatcher(ctx context.Context, projectID, location, queue, targetURL, saEmail, audience string) (*Dispatcher, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(location) == "" || strings.TrimSpace(queue) == "" || strings.TrimSpace(targetURL) == "" {
		return nil, fmt.Errorf("projectID, location, queue, and targetURL are required")
	}
	client, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(audience) == "" {
		audience = targetAudienceFromURL(targetURL)
	}
	return &Dispatcher{
		client:    client,
		projectID: projectID,
		location:  location,
		queue:     queue,
		targetURL: targetURL,
		saEmail:   saEmail,
		audience:  audience,
	}, nil
}

func (d *Dispatcher) Close() error {
	if d == nil || d.client == nil {
		return nil
	}
	return d.client.Close()
}

func (d *Dispatcher) EnqueueTask(ctx context.Context, taskID string) error {
	if d == nil || d.client == nil {
		return fmt.Errorf("dispatcher is not configured")
	}
	body, err := json.Marshal(TaskPayload{TaskID: strings.TrimSpace(taskID)})
	if err != nil {
		return err
	}
	parent := fmt.Sprintf("projects/%s/locations/%s/queues/%s", d.projectID, d.location, d.queue)
	httpRequest := &taskspb.HttpRequest{
		HttpMethod: taskspb.HttpMethod_POST,
		Url:        d.targetURL,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}
	if strings.TrimSpace(d.saEmail) != "" {
		httpRequest.AuthorizationHeader = &taskspb.HttpRequest_OidcToken{
			OidcToken: &taskspb.OidcToken{
				ServiceAccountEmail: d.saEmail,
				Audience:            d.audience,
			},
		}
	}
	_, err = d.client.CreateTask(ctx, &taskspb.CreateTaskRequest{
		Parent: parent,
		Task: &taskspb.Task{
			MessageType: &taskspb.Task_HttpRequest{HttpRequest: httpRequest},
			ScheduleTime: timestamppb.New(time.Now().UTC()),
		},
	})
	return err
}

func targetAudienceFromURL(rawURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return trimmed
}
