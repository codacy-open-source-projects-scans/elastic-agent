// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package fleet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent/internal/pkg/agent/errors"
	"github.com/elastic/elastic-agent/internal/pkg/fleetapi"
	"github.com/elastic/elastic-agent/pkg/core/logger"
)

type ackRequest struct {
	Events []fleetapi.AckEvent `json:"events"`
}

type testAgentInfo struct{}

func (testAgentInfo) AgentID() string { return "agent-secret" }

type testSender struct {
	req *ackRequest
}

func (s *testSender) Send(
	_ context.Context,
	method string,
	path string,
	params url.Values,
	headers http.Header,
	body io.Reader,
) (*http.Response, error) {
	d := json.NewDecoder(body)
	var req ackRequest
	err := d.Decode(&req)
	if err != nil {
		return nil, err
	}
	s.req = &req
	return wrapStrToResp(http.StatusOK, `{ "action": "acks" }`), nil
}

func (s *testSender) URI() string {
	return "http://localhost"
}

func wrapStrToResp(code int, body string) *http.Response {
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", code, http.StatusText(code)),
		StatusCode:    code,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Body:          io.NopCloser(bytes.NewBufferString(body)),
		ContentLength: int64(len(body)),
		Header:        make(http.Header),
	}
}

func TestAcker_Ack(t *testing.T) {
	tests := []struct {
		name    string
		actions []fleetapi.Action
		batch   bool
	}{
		{
			name:    "nil",
			actions: nil,
		},
		{
			name:    "empty",
			actions: []fleetapi.Action{},
		},
		{
			name:    "ack",
			actions: []fleetapi.Action{&fleetapi.ActionUnknown{ActionID: "ack-test-action-id", ActionType: fleetapi.ActionTypeUnknown}},
		},
		{
			name: "ackbatch",
			actions: []fleetapi.Action{
				&fleetapi.ActionUnknown{ActionID: "ack-test-action-id1", ActionType: fleetapi.ActionTypeUnknown},
				&fleetapi.ActionUnknown{ActionID: "ack-test-action-id2", ActionType: fleetapi.ActionTypeUnknown},
			},
		},
		{
			name: "ackaction",
			actions: []fleetapi.Action{
				&fleetapi.ActionApp{
					ActionID:    "1b12dcd8-bde0-4045-92dc-c4b27668d733",
					InputType:   "osquery",
					Data:        []byte(`{"query":"select * from osquery_info"}`),
					Response:    map[string]interface{}{"osquery": map[string]interface{}{"count": float64(1)}},
					StartedAt:   "2022-02-23T18:26:08.506128Z",
					CompletedAt: "2022-02-23T18:26:08.507593Z",
				},
				&fleetapi.ActionApp{
					ActionID:    "2b12dcd8-bde0-4045-92dc-c4b27668d733",
					InputType:   "osquery",
					Data:        []byte(`{"query":"select * from foobar"}`),
					StartedAt:   "2022-02-24T18:26:08.506128Z",
					CompletedAt: "2022-02-24T18:26:08.507593Z",
					Error:       "uknown table",
				},
			},
		},
		{
			name: "ackupgrade",
			actions: []fleetapi.Action{
				&fleetapi.ActionUpgrade{
					ActionID:   "upgrade-ok",
					ActionType: fleetapi.ActionTypeUpgrade,
				},
				&fleetapi.ActionUpgrade{
					ActionID:   "upgrade-retry",
					ActionType: fleetapi.ActionTypeUpgrade,
					Data: fleetapi.ActionUpgradeData{
						Retry: 1,
					},
					Err: errors.New("upgrade failed"),
				},
				&fleetapi.ActionUpgrade{
					ActionID:   "upgrade-failed",
					ActionType: fleetapi.ActionTypeUpgrade,
					Data: fleetapi.ActionUpgradeData{
						Retry: -1,
					},
					Err: errors.New("upgrade failed"),
				},
			},
		},
	}

	log, _ := logger.New("fleet_acker", false)
	agentInfo := &testAgentInfo{}

	checkRequest := func(t *testing.T, actions []fleetapi.Action, req *ackRequest) {
		if len(actions) == 0 { // If no actions, expect no request, the sender was not called
			assert.Nil(t, req)
			return
		}
		assert.EqualValues(t, len(actions), len(req.Events))
		for i, ac := range actions {
			assert.EqualValues(t, "ACTION_RESULT", req.Events[i].EventType)
			assert.EqualValues(t, "ACKNOWLEDGED", req.Events[i].SubType)
			assert.EqualValues(t, ac.ID(), req.Events[i].ActionID)
			assert.EqualValues(t, agentInfo.AgentID(), req.Events[i].AgentID)
			assert.EqualValues(t, fmt.Sprintf("Action %q of type %q acknowledged.", ac.ID(), ac.Type()), req.Events[i].Message)
			// Check if the fleet acker handles RetryableActions correctly using the UpgradeAction
			if a, ok := ac.(*fleetapi.ActionUpgrade); ok {
				if a.Err != nil {
					assert.EqualValues(t, a.Err.Error(), req.Events[i].Error)
					// Check payload
					require.NotEmpty(t, req.Events[i].Payload)
					var pl struct {
						Retry   bool `json:"retry"`
						Attempt int  `json:"retry_attempt,omitempty"`
					}
					err := json.Unmarshal(req.Events[i].Payload, &pl)
					require.NoError(t, err)
					assert.Equal(t, a.Data.Retry, pl.Attempt,
						"action ID %s failed", a.ActionID)
					// Check retry flag
					if pl.Attempt > 0 {
						assert.True(t, pl.Retry)
					} else {
						assert.False(t, pl.Retry)
					}
				} else {
					assert.Empty(t, req.Events[i].Error)
				}
			}
			if a, ok := ac.(*fleetapi.ActionApp); ok {
				assert.EqualValues(t, a.InputType, req.Events[i].ActionInputType)
				assert.EqualValues(t, a.Data, req.Events[i].ActionData)
				assert.EqualValues(t, a.Response, req.Events[i].ActionResponse)
				assert.EqualValues(t, a.StartedAt, req.Events[i].StartedAt)
				assert.EqualValues(t, a.CompletedAt, req.Events[i].CompletedAt)
				assert.EqualValues(t, a.Error, req.Events[i].Error)
			}

		}
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sender := &testSender{}
			acker, err := NewAcker(log, agentInfo, sender)
			require.NoError(t, err)
			require.NotNil(t, acker, "acker not initialized")

			if len(tc.actions) == 1 {
				err = acker.Ack(context.Background(), tc.actions[0])
			} else {
				_, err = acker.AckBatch(context.Background(), tc.actions)
			}
			require.NoError(t, err)

			err = acker.Commit(context.Background())
			require.NoError(t, err)

			checkRequest(t, tc.actions, sender.req)
		})
	}
}
