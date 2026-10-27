// Copyright 2026 Palantir Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-github/v90/github"
	"github.com/palantir/policy-bot/policy/common"
	"github.com/palantir/policy-bot/pull"
	"github.com/palantir/policy-bot/pull/pulltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConfigSuppressesStatusForUnseenPolicy(t *testing.T) {
	ec := makeEvalContext(false)

	evaluator, err := ec.ParseConfig(context.Background(), common.TriggerAll)
	require.Error(t, err)
	assert.Nil(t, evaluator)
	assert.Nil(t, ec.Status)
}

func TestParseConfigPostsStatusForSeenPolicy(t *testing.T) {
	ec := makeEvalContext(true)

	evaluator, err := ec.ParseConfig(context.Background(), common.TriggerAll)
	require.Error(t, err)
	assert.Nil(t, evaluator)
	require.NotNil(t, ec.Status)
	assert.Equal(t, "error", ec.Status.GetState())
	assert.Equal(t, "policy-bot: main", ec.Status.GetContext())
	assert.Equal(t, "Error loading policy from testorg/testrepo@main", ec.Status.GetDescription())
}

func TestParseConfigPostsBareContextForDefaultBranchPolicy(t *testing.T) {
	ec := makeEvalContext(true)
	ec.Options.PolicyFromDefaultBranch = true

	evaluator, err := ec.ParseConfig(context.Background(), common.TriggerAll)
	require.Error(t, err)
	assert.Nil(t, evaluator)
	require.NotNil(t, ec.Status)
	assert.Equal(t, "policy-bot", ec.Status.GetContext())
}

func TestEvaluatePolicyFailsWhenHeadIsSharedWithDefaultBranchPolicy(t *testing.T) {
	approved := &stubEvaluator{result: common.Result{Status: common.StatusApproved, StatusDescription: "All rules are approved"}}

	pending := &stubEvaluator{result: common.Result{Status: common.StatusPending, StatusDescription: "0/1 rules approved"}}

	for name, evaluator := range map[string]*stubEvaluator{"sharedHeadApproved": approved, "sharedHeadPending": pending} {
		t.Run(name, func(t *testing.T) {
			ec := makeEvalContext(true)
			ec.Options.PolicyFromDefaultBranch = true
			ec.Client = pullsForCommitClient(t, http.StatusOK, `[
				{"number": 42, "state": "open", "head": {"sha": "abc123"}},
				{"number": 7, "state": "open", "head": {"sha": "abc123"}}
			]`)

			_, err := ec.EvaluatePolicy(context.Background(), evaluator)
			require.NoError(t, err)
			require.NotNil(t, ec.Status)
			assert.Equal(t, "failure", ec.Status.GetState())
			assert.Equal(t, "policy-bot", ec.Status.GetContext())
			assert.Contains(t, ec.Status.GetDescription(), "#7")
		})
	}

	t.Run("onlyUnrelatedPulls", func(t *testing.T) {
		ec := makeEvalContext(true)
		ec.Options.PolicyFromDefaultBranch = true
		ec.Client = pullsForCommitClient(t, http.StatusOK, `[
			{"number": 42, "state": "open", "head": {"sha": "abc123"}},
			{"number": 8, "state": "open", "head": {"sha": "def456"}},
			{"number": 9, "state": "closed", "head": {"sha": "abc123"}}
		]`)

		_, err := ec.EvaluatePolicy(context.Background(), approved)
		require.NoError(t, err)
		require.NotNil(t, ec.Status)
		assert.Equal(t, "success", ec.Status.GetState())
	})

	t.Run("listError", func(t *testing.T) {
		ec := makeEvalContext(true)
		ec.Options.PolicyFromDefaultBranch = true
		ec.Client = pullsForCommitClient(t, http.StatusInternalServerError, `{"message": "boom"}`)

		_, err := ec.EvaluatePolicy(context.Background(), approved)
		require.Error(t, err)
		require.NotNil(t, ec.Status)
		assert.Equal(t, "error", ec.Status.GetState())
	})

	t.Run("branchContextSkipsCheck", func(t *testing.T) {
		ec := makeEvalContext(true) // nil client: any API call panics

		_, err := ec.EvaluatePolicy(context.Background(), approved)
		require.NoError(t, err)
		require.NotNil(t, ec.Status)
		assert.Equal(t, "success", ec.Status.GetState())
		assert.Equal(t, "policy-bot: main", ec.Status.GetContext())
	})
}

type stubEvaluator struct {
	result common.Result
}

func (e *stubEvaluator) Trigger() common.Trigger { return common.TriggerAll }

func (e *stubEvaluator) Evaluate(context.Context, pull.Context) common.Result { return e.result }

func pullsForCommitClient(t *testing.T, statusCode int, body string) *github.Client {
	client, err := github.NewClient(
		github.WithTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, "/repos/testorg/testrepo/commits/abc123/pulls", req.URL.Path)
			return &http.Response{
				StatusCode: statusCode,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		})),
	)
	require.NoError(t, err)
	return client
}

func makeEvalContext(seenPolicy bool) *EvalContext {
	return &EvalContext{
		Options: &PullEvaluationOptions{
			StatusCheckContext: "policy-bot",
		},
		PublicURL: "https://policy-bot.example.com",
		PullContext: &pulltest.Context{
			OwnerValue:     "testorg",
			RepoValue:      "testrepo",
			NumberValue:    42,
			StateValue:     "open",
			HeadSHAValue:   "abc123",
			BranchBaseName: "main",
			BranchHeadName: "feature",
		},
		SkipPostStatus: true,
		Config: FetchedConfig{
			LoadError:  errors.New("request failed"),
			Source:     "testorg/testrepo@main",
			Path:       ".policy.yml",
			SeenPolicy: seenPolicy,
		},
	}
}
