// Copyright 2020 Red Hat, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/RedHatInsights/insights-results-aggregator/metrics"
	"github.com/RedHatInsights/insights-results-aggregator/types"
)

// UserVote is a type for user's vote
type UserVote int

const (
	// UserVoteDislike shows user's dislike
	UserVoteDislike UserVote = -1
	// UserVoteNone shows no vote from user
	UserVoteNone UserVote = 0
	// UserVoteLike shows user's like
	UserVoteLike UserVote = 1
)

// UserFeedbackOnRule shows user's feedback on rule
type UserFeedbackOnRule struct {
	ClusterID types.ClusterName
	RuleID    types.RuleID
	UserID    types.UserID
	Message   string
	UserVote  UserVote
	AddedAt   time.Time
	UpdatedAt time.Time
}

// VoteOnRule likes or dislikes rule for cluster by user. If entry exists, it overwrites it
func (storage DBStorage) VoteOnRule(
	clusterID types.ClusterName,
	ruleID types.RuleID,
	userID types.UserID,
	userVote UserVote,
) error {
	return storage.addOrUpdateUserFeedbackOnRuleForCluster(clusterID, ruleID, userID, &userVote, nil)
}

// AddOrUpdateFeedbackOnRule adds feedback on rule for cluster by user. If entry exists, it overwrites it
func (storage DBStorage) AddOrUpdateFeedbackOnRule(
	clusterID types.ClusterName,
	ruleID types.RuleID,
	userID types.UserID,
	message string,
) error {
	return storage.addOrUpdateUserFeedbackOnRuleForCluster(clusterID, ruleID, userID, nil, &message)
}

// addOrUpdateUserFeedbackOnRuleForCluster adds or updates feedback
// will update user vote and messagePtr if the pointers are not nil
func (storage DBStorage) addOrUpdateUserFeedbackOnRuleForCluster(
	clusterID types.ClusterName,
	ruleID types.RuleID,
	userID types.UserID,
	userVotePtr *UserVote,
	messagePtr *string,
) error {
	updateVote := false
	updateMessage := false
	userVote := UserVoteNone
	message := ""

	if userVotePtr != nil {
		updateVote = true
		userVote = *userVotePtr
	}

	if messagePtr != nil {
		updateMessage = true
		message = *messagePtr
	}

	query, err := storage.constructUpsertClusterRuleUserFeedback(updateVote, updateMessage)
	if err != nil {
		return err
	}

	statement, err := storage.connection.Prepare(query)
	if err != nil {
		return err
	}
	defer func() {
		err := statement.Close()
		if err != nil {
			log.Error().Err(err).Msg("Unable to close statement")
		}
	}()

	now := time.Now()

	_, err = statement.Exec(clusterID, ruleID, userID, userVote, now, now, message)
	if err != nil {
		log.Error().Err(err).Msg("addOrUpdateUserFeedbackOnRuleForCluster")
		return err
	}

	metrics.FeedbackOnRules.Inc()

	return nil
}

func (storage DBStorage) constructUpsertClusterRuleUserFeedback(updateVote bool, updateMessage bool) (string, error) {
	var query string

	switch storage.dbDriverType {
	case DBDriverSQLite3, DBDriverPostgres, DBDriverGeneral:
		query = `
			INSERT INTO cluster_rule_user_feedback
			(cluster_id, rule_id, user_id, user_vote, added_at, updated_at, message)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`

		var updates []string

		if updateVote {
			updates = append(updates, "user_vote = $4")
		}

		if updateMessage {
			updates = append(updates, "message = $7")
		}

		if len(updates) > 0 {
			updates = append(updates, "updated_at = $6")
			query += "ON CONFLICT (cluster_id, rule_id, user_id) DO UPDATE SET "
			query += strings.Join(updates, ", ")
		}
	default:
		return "", fmt.Errorf("DB driver %v is not supported", storage.dbDriverType)
	}

	return query, nil
}

// GetUserFeedbackOnRule gets user feedback from db
func (storage DBStorage) GetUserFeedbackOnRule(
	clusterID types.ClusterName, ruleID types.RuleID, userID types.UserID,
) (*UserFeedbackOnRule, error) {
	feedback := UserFeedbackOnRule{}

	err := storage.connection.QueryRow(
		`SELECT cluster_id, rule_id, user_id, message, user_vote, added_at, updated_at
		FROM cluster_rule_user_feedback
		WHERE cluster_id = $1 AND rule_id = $2 AND user_id = $3`,
		clusterID, ruleID, userID,
	).Scan(
		&feedback.ClusterID,
		&feedback.RuleID,
		&feedback.UserID,
		&feedback.Message,
		&feedback.UserVote,
		&feedback.AddedAt,
		&feedback.UpdatedAt,
	)

	switch {
	case err == sql.ErrNoRows:
		return nil, &ItemNotFoundError{
			ItemID: fmt.Sprintf("%v/%v/%v", clusterID, ruleID, userID),
		}
	case err != nil:
		return nil, err
	}

	return &feedback, nil
}
