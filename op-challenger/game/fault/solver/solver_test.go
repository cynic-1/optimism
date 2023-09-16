package solver

import (
	"context"
	"testing"

	"github.com/ethereum-optimism/optimism/op-challenger/game/fault/test"
	faulttest "github.com/ethereum-optimism/optimism/op-challenger/game/fault/test"
	"github.com/ethereum-optimism/optimism/op-challenger/game/fault/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestAttemptStep(t *testing.T) {
	maxDepth := 3
	claimBuilder := test.NewAlphabetClaimBuilder(t, maxDepth)

	// Last accessible leaf is the second last trace index
	// The root node is used for the last trace index and can only be attacked.
	lastLeafTraceIndex := uint64(1<<maxDepth - 2)
	ctx := context.Background()

	tests := []struct {
		name               string
		expectedErr        error
		expectAttack       bool
		expectPreState     []byte
		expectProofData    []byte
		expectedOracleData *types.PreimageOracleData
		rootClaimCorrect   bool
		setupGame          func(builder *faulttest.GameBuilder)
	}{
		{
			name:               "AttackFirstTraceIndex",
			expectAttack:       true,
			expectPreState:     claimBuilder.CorrectPreState(0),
			expectProofData:    claimBuilder.CorrectProofData(0),
			expectedOracleData: claimBuilder.CorrectOracleData(0),
			setupGame: func(builder *faulttest.GameBuilder) {
				builder.Seq().
					Attack(common.Hash{0xaa}).
					AttackCorrect().
					Attack(common.Hash{0xbb})
			},
			rootClaimCorrect: true,
		},
		{
			name:               "DefendFirstTraceIndex",
			expectAttack:       false,
			expectPreState:     claimBuilder.CorrectPreState(1),
			expectProofData:    claimBuilder.CorrectProofData(1),
			expectedOracleData: claimBuilder.CorrectOracleData(1),
			setupGame: func(builder *faulttest.GameBuilder) {
				builder.Seq().
					Attack(common.Hash{0xaa}).
					AttackCorrect().
					AttackCorrect()
			},
			rootClaimCorrect: true,
		},
		{
			name: "AttackMiddleTraceIndex",
			//claim:              claimBuilder.CreateLeafClaim(4, false),
			expectAttack:       true,
			expectPreState:     claimBuilder.CorrectPreState(4),
			expectProofData:    claimBuilder.CorrectProofData(4),
			expectedOracleData: claimBuilder.CorrectOracleData(4),
			setupGame: func(builder *faulttest.GameBuilder) {
				builder.Seq().
					AttackCorrect().
					DefendCorrect().
					Attack(common.Hash{0xaa})
			},
			rootClaimCorrect: true,
		},
		{
			name: "DefendMiddleTraceIndex",
			//claim:              claimBuilder.CreateLeafClaim(4, true),
			expectAttack:       false,
			expectPreState:     claimBuilder.CorrectPreState(5),
			expectProofData:    claimBuilder.CorrectProofData(5),
			expectedOracleData: claimBuilder.CorrectOracleData(5),
			setupGame: func(builder *faulttest.GameBuilder) {
				builder.Seq().
					AttackCorrect().
					DefendCorrect().
					AttackCorrect()
			},
			rootClaimCorrect: true,
		},
		{
			name:               "AttackLastTraceIndex",
			expectAttack:       true,
			expectPreState:     claimBuilder.CorrectPreState(lastLeafTraceIndex),
			expectProofData:    claimBuilder.CorrectProofData(lastLeafTraceIndex),
			expectedOracleData: claimBuilder.CorrectOracleData(lastLeafTraceIndex),
			setupGame: func(builder *faulttest.GameBuilder) {
				builder.Seq().
					AttackCorrect().
					DefendCorrect().
					Defend(common.Hash{0xaa})
			},
			rootClaimCorrect: true,
		},
		{
			name:               "DefendLastTraceIndex",
			expectAttack:       false,
			expectPreState:     claimBuilder.CorrectPreState(lastLeafTraceIndex + 1),
			expectProofData:    claimBuilder.CorrectProofData(lastLeafTraceIndex + 1),
			expectedOracleData: claimBuilder.CorrectOracleData(lastLeafTraceIndex + 1),
			setupGame: func(builder *faulttest.GameBuilder) {
				builder.Seq().
					AttackCorrect().
					DefendCorrect().
					DefendCorrect()
			},
			rootClaimCorrect: true,
		},
		{
			name: "CannotStepNonLeaf",
			setupGame: func(builder *faulttest.GameBuilder) {
				builder.Seq().AttackCorrect().AttackCorrect()
			},
			expectedErr: ErrStepNonLeafNode,
		},
		{
			name: "CannotStepAgreedNode",
			setupGame: func(builder *faulttest.GameBuilder) {
				builder.Seq().
					AttackCorrect().
					Attack(common.Hash{0xaa}).
					AttackCorrect()
			},
			expectedErr: ErrStepIgnoreInvalidPath,
		},
		{
			name: "CannotStepInvalidPath",
			setupGame: func(builder *faulttest.GameBuilder) {
				builder.Seq().
					Attack(common.Hash{0xaa}).
					Attack(common.Hash{0xbb}).
					Attack(common.Hash{0xcc})
			},
			expectedErr: ErrStepIgnoreInvalidPath,
		},
		{
			name:               "CannotStepNearlyValidPath",
			expectAttack:       true,
			expectPreState:     claimBuilder.CorrectPreState(4),
			expectProofData:    claimBuilder.CorrectProofData(4),
			expectedOracleData: claimBuilder.CorrectOracleData(4),
			setupGame: func(builder *faulttest.GameBuilder) {
				builder.Seq().
					AttackCorrect().
					DefendCorrect().
					DefendCorrect()
			},
			expectedErr: ErrStepIgnoreInvalidPath,
		},
	}

	for _, tableTest := range tests {
		tableTest := tableTest
		t.Run(tableTest.name, func(t *testing.T) {
			builder := claimBuilder.GameBuilder(tableTest.rootClaimCorrect)
			tableTest.setupGame(builder)
			alphabetSolver := newClaimSolver(maxDepth, claimBuilder.CorrectTraceProvider())
			game := builder.Game
			claims := game.Claims()
			lastClaim := claims[len(claims)-1]
			step, err := alphabetSolver.AttemptStep(ctx, game, lastClaim)
			if tableTest.expectedErr == nil {
				require.NoError(t, err)
				require.Equal(t, lastClaim, step.LeafClaim)
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
