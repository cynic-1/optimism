package disputegame

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum-optimism/optimism/op-bindings/bindings"
	"github.com/ethereum-optimism/optimism/op-challenger/game/fault"
	"github.com/ethereum-optimism/optimism/op-challenger/game/fault/types"
	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/wait"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"
)

type FaultGameHelper struct {
	t           *testing.T
	require     *require.Assertions
	client      *ethclient.Client
	opts        *bind.TransactOpts
	game        *bindings.FaultDisputeGame
	factoryAddr common.Address
	addr        common.Address
}

func (g *FaultGameHelper) Addr() common.Address {
	return g.addr
}

func (g *FaultGameHelper) GameDuration(ctx context.Context) time.Duration {
	duration, err := g.game.GAMEDURATION(&bind.CallOpts{Context: ctx})
	g.require.NoError(err, "failed to get game duration")
	return time.Duration(duration) * time.Second
}

func (g *FaultGameHelper) WaitForClaimCount(ctx context.Context, count int64) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := wait.For(ctx, time.Second, func() (bool, error) {
		actual, err := g.game.ClaimDataLen(&bind.CallOpts{Context: ctx})
		if err != nil {
			return false, err
		}
		g.t.Log("Waiting for claim count", "current", actual, "expected", count, "game", g.addr)
		return actual.Cmp(big.NewInt(count)) == 0, nil
	})
	g.require.NoErrorf(err, "Did not find expected claim count %v", count)
}

type ContractClaim struct {
	ParentIndex uint32
	Countered   bool
	Claim       [32]byte
	Position    *big.Int
	Clock       *big.Int
}

func (g *FaultGameHelper) MaxDepth(ctx context.Context) int64 {
	depth, err := g.game.MAXGAMEDEPTH(&bind.CallOpts{Context: ctx})
	g.require.NoError(err, "Failed to load game depth")
	return depth.Int64()
}

func (g *FaultGameHelper) waitForClaim(ctx context.Context, errorMsg string, predicate func(claim ContractClaim) bool) {
	timedCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := wait.For(timedCtx, time.Second, func() (bool, error) {
		count, err := g.game.ClaimDataLen(&bind.CallOpts{Context: timedCtx})
		if err != nil {
			return false, fmt.Errorf("retrieve number of claims: %w", err)
		}
		// Search backwards because the new claims are at the end and more likely the ones we want.
		for i := count.Int64() - 1; i >= 0; i-- {
			claimData, err := g.game.ClaimData(&bind.CallOpts{Context: timedCtx}, big.NewInt(i))
			if err != nil {
				return false, fmt.Errorf("retrieve claim %v: %w", i, err)
			}
			if predicate(claimData) {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil { // Avoid waiting time capturing game data when there's no error
		g.require.NoErrorf(err, "%v\n%v", errorMsg, g.gameData(ctx))
	}
}

func (g *FaultGameHelper) waitForNoClaim(ctx context.Context, errorMsg string, predicate func(claim ContractClaim) bool) {
	timedCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	err := wait.For(timedCtx, time.Second, func() (bool, error) {
		count, err := g.game.ClaimDataLen(&bind.CallOpts{Context: timedCtx})
		if err != nil {
			return false, fmt.Errorf("retrieve number of claims: %w", err)
		}
		// Search backwards because the new claims are at the end and more likely the ones we will fail on.
		for i := count.Int64() - 1; i >= 0; i-- {
			claimData, err := g.game.ClaimData(&bind.CallOpts{Context: timedCtx}, big.NewInt(i))
			if err != nil {
				return false, fmt.Errorf("retrieve claim %v: %w", i, err)
			}
			if predicate(claimData) {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil { // Avoid waiting time capturing game data when there's no error
		g.require.NoErrorf(err, "%v\n%v", errorMsg, g.gameData(ctx))
	}
}

func (g *FaultGameHelper) GetClaimValue(ctx context.Context, claimIdx int64) common.Hash {
	g.WaitForClaimCount(ctx, claimIdx+1)
	claim := g.getClaim(ctx, claimIdx)
	return claim.Claim
}

// getClaim retrieves the claim data for a specific index.
// Note that it is deliberately not exported as tests should use WaitForClaim to avoid race conditions.
func (g *FaultGameHelper) getClaim(ctx context.Context, claimIdx int64) ContractClaim {
	claimData, err := g.game.ClaimData(&bind.CallOpts{Context: ctx}, big.NewInt(claimIdx))
	if err != nil {
		g.require.NoErrorf(err, "retrieve claim %v", claimIdx)
	}
	return claimData
}

func (g *FaultGameHelper) GetClaimUnsafe(ctx context.Context, claimIdx int64) ContractClaim {
	return g.getClaim(ctx, claimIdx)
}

func (g *FaultGameHelper) WaitForClaimAtDepth(ctx context.Context, depth int) {
	g.waitForClaim(
		ctx,
		fmt.Sprintf("Could not find claim depth %v", depth),
		func(claim ContractClaim) bool {
			pos := types.NewPositionFromGIndex(claim.Position.Uint64())
			return pos.Depth() == depth
		})
}

func (g *FaultGameHelper) WaitForClaimAtMaxDepth(ctx context.Context, countered bool) {
	maxDepth := g.MaxDepth(ctx)
	g.waitForClaim(
		ctx,
		fmt.Sprintf("Could not find claim depth %v with countered=%v", maxDepth, countered),
		func(claim ContractClaim) bool {
			pos := types.NewPositionFromGIndex(claim.Position.Uint64())
			return int64(pos.Depth()) == maxDepth && claim.Countered == countered
		})
}

func (g *FaultGameHelper) WaitForAllClaimsCountered(ctx context.Context) {
	g.waitForNoClaim(
		ctx,
		"Did not find all claims countered",
		func(claim ContractClaim) bool {
			return !claim.Countered
		})
}

func (g *FaultGameHelper) Resolve(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	tx, err := g.game.Resolve(g.opts)
	g.require.NoError(err)
	_, err = wait.ForReceiptOK(ctx, g.client, tx.Hash())
	g.require.NoError(err)
}

func (g *FaultGameHelper) WaitForGameStatus(ctx context.Context, expected Status) {
	g.t.Logf("Waiting for game %v to have status %v", g.addr, expected)
	timedCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	err := wait.For(timedCtx, time.Second, func() (bool, error) {
		ctx, cancel := context.WithTimeout(timedCtx, 30*time.Second)
		defer cancel()
		status, err := g.game.Status(&bind.CallOpts{Context: ctx})
		if err != nil {
			return false, fmt.Errorf("game status unavailable: %w", err)
		}
		g.t.Logf("Game %v has state %v, waiting for state %v", g.addr, Status(status), expected)
		return expected == Status(status), nil
	})
	g.require.NoErrorf(err, "wait for game status. Game state: \n%v", g.gameData(ctx))
}

func (g *FaultGameHelper) WaitForInactivity(ctx context.Context, numInactiveBlocks int) {
	g.t.Logf("Waiting for game %v to have no activity for %v blocks", g.addr, numInactiveBlocks)
	headCh := make(chan *gethtypes.Header, 100)
	headSub, err := g.client.SubscribeNewHead(ctx, headCh)
	g.require.NoError(err)
	defer headSub.Unsubscribe()

	var lastActiveBlock uint64
	for {
		select {
		case head := <-headCh:
			if lastActiveBlock == 0 {
				lastActiveBlock = head.Number.Uint64()
				continue
			} else if lastActiveBlock+uint64(numInactiveBlocks) > head.Number.Uint64() {
				return
			}
			block, err := g.client.BlockByNumber(ctx, head.Number)
			g.require.NoError(err)
			numActions := 0
			for _, tx := range block.Transactions() {
				if tx.To().Hex() == g.addr.Hex() {
					numActions++
				}
			}
			if numActions != 0 {
				g.t.Logf("Game %v has %v actions in block %d. Resetting inactivity timeout", g.addr, numActions, block.NumberU64())
				lastActiveBlock = head.Number.Uint64()
			}
		case err := <-headSub.Err():
			g.require.NoError(err)
		case <-ctx.Done():
			g.require.Fail("Context canceled", ctx.Err())
		}
	}
}

// Mover is a function that either attacks or defends the claim at parentClaimIdx
type Mover func(parentClaimIdx int64)

// Stepper is a function that attempts to perform a step against the claim at parentClaimIdx
type Stepper func(parentClaimIdx int64)

// DefendRootClaim uses the supplied Mover to perform moves in an attempt to defend the root claim.
// It is assumed that the output root being disputed is valid and that an honest op-challenger is already running.
// When the game has reached the maximum depth it waits for the honest challenger to counter the leaf claim with step.
func (g *FaultGameHelper) DefendRootClaim(ctx context.Context, performMove Mover) {
	maxDepth := g.MaxDepth(ctx)
	for claimCount := int64(1); claimCount < maxDepth; {
		g.LogGameData(ctx)
		claimCount++
		// Wait for the challenger to counter
		g.WaitForClaimCount(ctx, claimCount)

		// Respond with our own move
		performMove(claimCount - 1)
		claimCount++
		g.WaitForClaimCount(ctx, claimCount)
	}

	// Wait for the challenger to call step and counter our invalid claim
	g.WaitForClaimAtMaxDepth(ctx, true)
}

// ChallengeRootClaim uses the supplied Mover and Stepper to perform moves and steps in an attempt to challenge the root claim.
// It is assumed that the output root being disputed is invalid and that an honest op-challenger is already running.
// When the game has reached the maximum depth it calls the Stepper to attempt to counter the leaf claim.
// Since the output root is invalid, it should not be possible for the Stepper to call step successfully.
func (g *FaultGameHelper) ChallengeRootClaim(ctx context.Context, performMove Mover, attemptStep Stepper) {
	maxDepth := g.MaxDepth(ctx)
	for claimCount := int64(1); claimCount < maxDepth; {
		g.LogGameData(ctx)
		// Perform our move
		performMove(claimCount - 1)
		claimCount++
		g.WaitForClaimCount(ctx, claimCount)

		// Wait for the challenger to counter
		claimCount++
		g.WaitForClaimCount(ctx, claimCount)
	}

	// Confirm the game has reached max depth and the last claim hasn't been countered
	g.WaitForClaimAtMaxDepth(ctx, false)
	g.LogGameData(ctx)

	// It's on us to call step if we want to win but shouldn't be possible
	attemptStep(maxDepth)
}

func (g *FaultGameHelper) WaitForNewClaim(ctx context.Context, checkPoint int64) (int64, error) {
	timedCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var newClaimLen int64
	err := wait.For(timedCtx, time.Second, func() (bool, error) {
		actual, err := g.game.ClaimDataLen(&bind.CallOpts{Context: ctx})
		if err != nil {
			return false, err
		}
		newClaimLen = actual.Int64()
		return actual.Cmp(big.NewInt(checkPoint)) > 0, nil
	})
	return newClaimLen, err
}

func (g *FaultGameHelper) Attack(ctx context.Context, claimIdx int64, claim common.Hash) {
	tx, err := g.game.Attack(g.opts, big.NewInt(claimIdx), claim)
	g.LogGameData(ctx)
	g.t.Logf("Attack. claim: %v. claimIdx: %d", claim, claimIdx)
	g.require.NoError(err, "Attack transaction did not send")
	g.t.Logf("Attack tx: %x. claim: %v. claimIdx: %d", tx.Hash(), claim, claimIdx)
	_, err = wait.ForReceiptOK(ctx, g.client, tx.Hash())
	g.require.NoError(err, "Attack transaction was not OK")
}

func (g *FaultGameHelper) Defend(ctx context.Context, claimIdx int64, claim common.Hash) {
	tx, err := g.game.Defend(g.opts, big.NewInt(claimIdx), claim)
	g.require.NoError(err, "Defend transaction did not send")
	_, err = wait.ForReceiptOK(ctx, g.client, tx.Hash())
	g.require.NoError(err, "Defend transaction was not OK")
}

type ErrWithData interface {
	ErrorData() interface{}
}

// StepFails attempts to call step and verifies that it fails with ValidStep()
func (g *FaultGameHelper) StepFails(claimIdx int64, isAttack bool, stateData []byte, proof []byte) {
	g.t.Logf("Attempting step against claim %v isAttack: %v", claimIdx, isAttack)
	_, err := g.game.Step(g.opts, big.NewInt(claimIdx), isAttack, stateData, proof)
	errData, ok := err.(ErrWithData)
	g.require.Truef(ok, "Error should provide ErrorData method: %v", err)
	g.require.Equal("0xfb4e40dd", errData.ErrorData(), "Revert reason should be abi encoded ValidStep()")
}

// ResolveClaim resolves a single subgame
func (g *FaultGameHelper) ResolveClaim(ctx context.Context, claimIdx int64) {
	tx, err := g.game.ResolveClaim(g.opts, big.NewInt(claimIdx))
	g.require.NoError(err, "ResolveClaim transaction did not send")
	_, err = wait.ForReceiptOK(ctx, g.client, tx.Hash())
	g.require.NoError(err, "ResolveClaim transaction was not OK")
}

// ResolveAllClaims resolves all subgames
// This function does not resolve the game. That's the responsibility of challengers
func (g *FaultGameHelper) ResolveAllClaims(ctx context.Context) {
	loader := fault.NewLoader(g.game)
	claims, err := loader.FetchClaims(ctx)
	g.require.NoError(err, "Failed to fetch claims")
	subgames := make(map[int]bool)
	for i := len(claims) - 1; i > 0; i-- {
		subgames[claims[i].ParentContractIndex] = true
		// Subgames containing only one node are implicitly resolved
		// i.e. uncountered and claims at MAX_DEPTH
		if !subgames[i] {
			continue
		}
		g.ResolveClaim(ctx, int64(i))
	}
	g.ResolveClaim(ctx, 0)
}

func (g *FaultGameHelper) gameData(ctx context.Context) string {
	opts := &bind.CallOpts{Context: ctx}
	maxDepth := int(g.MaxDepth(ctx))
	claimCount, err := g.game.ClaimDataLen(opts)
	info := fmt.Sprintf("Claim count: %v\n", claimCount)
	g.require.NoError(err, "Fetching claim count")
	for i := int64(0); i < claimCount.Int64(); i++ {
		claim, err := g.game.ClaimData(opts, big.NewInt(i))
		g.require.NoErrorf(err, "Fetch claim %v", i)

		pos := types.NewPositionFromGIndex(claim.Position.Uint64())
		info = info + fmt.Sprintf("%v - Position: %v, Depth: %v, IndexAtDepth: %v Trace Index: %v, Value: %v, Countered: %v\n",
			i, claim.Position.Int64(), pos.Depth(), pos.IndexAtDepth(), pos.TraceIndex(maxDepth), common.Hash(claim.Claim).Hex(), claim.Countered)
	}
	status, err := g.game.Status(opts)
	g.require.NoError(err, "Load game status")
	return fmt.Sprintf("Game %v (%v):\n%v\n", g.addr, Status(status), info)
}

func (g *FaultGameHelper) LogGameData(ctx context.Context) {
	g.t.Log(g.gameData(ctx))
}

type dishonestClaim struct {
	ParentIndex int64
	IsAttack    bool
	Valid       bool
}
type DishonestHelper struct {
	*FaultGameHelper
	*HonestHelper
	claims   map[dishonestClaim]bool
	defender bool
}

func NewDishonestHelper(g *FaultGameHelper, correctTrace *HonestHelper, defender bool) *DishonestHelper {
	return &DishonestHelper{g, correctTrace, make(map[dishonestClaim]bool), defender}
}

func (t *DishonestHelper) Attack(ctx context.Context, claimIndex int64) {
	c := dishonestClaim{claimIndex, true, false}
	if t.claims[c] {
		return
	}
	t.claims[c] = true
	t.FaultGameHelper.Attack(ctx, claimIndex, common.Hash{byte(claimIndex)})
}

func (t *DishonestHelper) Defend(ctx context.Context, claimIndex int64) {
	c := dishonestClaim{claimIndex, false, false}
	if t.claims[c] {
		return
	}
	t.claims[c] = true
	t.FaultGameHelper.Defend(ctx, claimIndex, common.Hash{byte(claimIndex)})
}

func (t *DishonestHelper) AttackCorrect(ctx context.Context, claimIndex int64) {
	c := dishonestClaim{claimIndex, true, true}
	if t.claims[c] {
		return
	}
	t.claims[c] = true
	t.HonestHelper.Attack(ctx, claimIndex)
}

func (t *DishonestHelper) DefendCorrect(ctx context.Context, claimIndex int64) {
	c := dishonestClaim{claimIndex, false, true}
	if t.claims[c] {
		return
	}
	t.claims[c] = true
	t.HonestHelper.Defend(ctx, claimIndex)
}

// ExhaustDishonestClaims makes all possible significant moves (mod honest challenger's) in a game.
// It is very inefficient and should NOT be used on games with large depths
// However, it's still limited in that it does not generate valid claims at honest levels in an attempt to
// trick challengers into supporting poisoned paths.
func (d *DishonestHelper) ExhaustDishonestClaims(ctx context.Context) {
	depth := d.MaxDepth(ctx)

	move := func(claimIndex int64, claimData ContractClaim) {
		// dishonest level, valid attack
		// dishonest level, valid defense
		// honest level, invalid attack
		// honest level, invalid defense

		pos := types.NewPositionFromGIndex(claimData.Position.Uint64())
		if int64(pos.Depth()) == depth {
			return
		}

		d.FaultGameHelper.t.Logf("Dishonest Move at %d", claimIndex)
		d.LogGameData(ctx) // TODO: DEBUGME
		agree := d.defender == (pos.Depth()%2 == 0)
		if !agree {
			d.AttackCorrect(ctx, claimIndex)
			d.DefendCorrect(ctx, claimIndex)
		}
		d.Attack(ctx, claimIndex)
		if claimIndex != 0 {
			d.Defend(ctx, claimIndex)
		}
	}

	var numClaimsSeen int64
	for {
		newCount, err := d.WaitForNewClaim(ctx, numClaimsSeen)
		if errors.Is(err, context.DeadlineExceeded) {
			// we assume that the honest challenger has stopped responding
			// There's nothing to respond to.
			break
		}
		d.FaultGameHelper.require.NoError(err)

		for i := numClaimsSeen; i < newCount; i++ {
			claimData := d.getClaim(ctx, numClaimsSeen)
			move(numClaimsSeen, claimData)
			numClaimsSeen++
		}
	}
}
