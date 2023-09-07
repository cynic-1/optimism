package solver

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum-optimism/optimism/op-challenger/game/fault/test"
	"github.com/ethereum-optimism/optimism/op-challenger/game/fault/types"
	"github.com/stretchr/testify/require"
)

func TestAttemptStep(t *testing.T) {
	maxDepth := 3
	builder := test.NewAlphabetClaimBuilder(t, maxDepth)

	// Last accessible leaf is the second last trace index
	// The root node is used for the last trace index and can only be attacked.
	lastLeafTraceIndex := uint64(1<<maxDepth - 2)

	errProvider := errors.New("provider error")

	ctx := context.Background()

	tests := []struct {
		name               string
		claim              types.Claim
		expectedErr        error
		expectAttack       bool
		expectPreState     []byte
		expectProofData    []byte
		expectedOracleData *types.PreimageOracleData
	}{
		{
			name:               "AttackFirstTraceIndex",
			claim:              builder.CreateLeafClaim(0, false),
			expectAttack:       true,
			expectPreState:     builder.CorrectPreState(0),
			expectProofData:    builder.CorrectProofData(0),
			expectedOracleData: builder.CorrectOracleData(0),
		},
		{
			name:               "DefendFirstTraceIndex",
			claim:              builder.CreateLeafClaim(0, true),
			expectAttack:       false,
			expectPreState:     builder.CorrectPreState(1),
			expectProofData:    builder.CorrectProofData(1),
			expectedOracleData: builder.CorrectOracleData(1),
		},
		{
			name:               "AttackMiddleTraceIndex",
			claim:              builder.CreateLeafClaim(4, false),
			expectAttack:       true,
			expectPreState:     builder.CorrectPreState(4),
			expectProofData:    builder.CorrectProofData(4),
			expectedOracleData: builder.CorrectOracleData(4),
		},
		{
			name:               "DefendMiddleTraceIndex",
			claim:              builder.CreateLeafClaim(4, true),
			expectAttack:       false,
			expectPreState:     builder.CorrectPreState(5),
			expectProofData:    builder.CorrectProofData(5),
			expectedOracleData: builder.CorrectOracleData(5),
		},
		{
			name:               "AttackLastTraceIndex",
			claim:              builder.CreateLeafClaim(lastLeafTraceIndex, false),
			expectAttack:       true,
			expectPreState:     builder.CorrectPreState(lastLeafTraceIndex),
			expectProofData:    builder.CorrectProofData(lastLeafTraceIndex),
			expectedOracleData: builder.CorrectOracleData(lastLeafTraceIndex),
		},
		{
			name:               "DefendLastTraceIndex",
			claim:              builder.CreateLeafClaim(lastLeafTraceIndex, true),
			expectAttack:       false,
			expectPreState:     builder.CorrectPreState(lastLeafTraceIndex + 1),
			expectProofData:    builder.CorrectProofData(lastLeafTraceIndex + 1),
			expectedOracleData: builder.CorrectOracleData(lastLeafTraceIndex + 1),
		},
		{
			name:        "CannotStepNonLeaf",
			claim:       builder.Seq(false).Attack(false).Get(),
			expectedErr: ErrStepNonLeafNode,
		},
		{
			name:        "CannotStepAgreedNode",
			claim:       builder.Seq(false).Attack(false).Get(),
			expectedErr: ErrStepNonLeafNode,
		},
		{
			name:        "CannotStepAgreedNode",
			claim:       builder.Seq(false).Attack(false).Get(),
			expectedErr: ErrStepNonLeafNode,
		},
	}

	for _, tableTest := range tests {
		tableTest := tableTest
		t.Run(tableTest.name, func(t *testing.T) {
			alphabetProvider := test.NewAlphabetWithProofProvider(t, maxDepth, nil)
			if errors.Is(tableTest.expectedErr, errProvider) {
				alphabetProvider = test.NewAlphabetWithProofProvider(t, maxDepth, errProvider)
			}
			builder = test.NewClaimBuilder(t, maxDepth, alphabetProvider)
			alphabetSolver := newClaimSolver(maxDepth, builder.CorrectTraceProvider())
			step, err := alphabetSolver.AttemptStep(ctx, tableTest.claim)
			if tableTest.expectedErr == nil {
				require.NoError(t, err)
				require.Equal(t, tableTest.claim, step.LeafClaim)
				require.Equal(t, tableTest.expectAttack, step.IsAttack)
				require.Equal(t, tableTest.expectPreState, step.PreState)
				require.Equal(t, tableTest.expectProofData, step.ProofData)
				require.Equal(t, tableTest.expectedOracleData.IsLocal, step.OracleData.IsLocal)
				require.Equal(t, tableTest.expectedOracleData.OracleKey, step.OracleData.OracleKey)
				require.Equal(t, tableTest.expectedOracleData.OracleData, step.OracleData.OracleData)
				require.Equal(t, tableTest.expectedOracleData.OracleOffset, step.OracleData.OracleOffset)
			} else {
				require.ErrorIs(t, err, tableTest.expectedErr)
				require.Equal(t, StepData{}, step)
			}
		})
	}
}
