package types

import (
	// it is ok to use math/rand here: we do not need a cryptographically secure random
	// number generator here and we can run the tests a bit faster
	"context"
	"crypto/rand"
	"encoding/hex"
	"math"
	mrand "math/rand"
	"os"
	"reflect"
	"testing"
	"time"

	gogotypes "github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ari-anchor/sei-tendermint/crypto"
	"github.com/ari-anchor/sei-tendermint/crypto/merkle"
	"github.com/ari-anchor/sei-tendermint/libs/bits"
	"github.com/ari-anchor/sei-tendermint/libs/bytes"
	tmrand "github.com/ari-anchor/sei-tendermint/libs/rand"
	tmtime "github.com/ari-anchor/sei-tendermint/libs/time"
	tmproto "github.com/ari-anchor/sei-tendermint/proto/tendermint/types"
	tmversion "github.com/ari-anchor/sei-tendermint/proto/tendermint/version"
	"github.com/ari-anchor/sei-tendermint/version"
)

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}

func TestBlockAddEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	txs := []Tx{Tx("foo"), Tx("bar")}
	lastID := makeBlockIDRandom()
	h := int64(3)

	voteSet, _, vals := randVoteSet(ctx, t, h-1, 1, tmproto.PrecommitType, 10, 1)
	extCommit, err := makeExtCommit(ctx, lastID, h-1, 1, voteSet, vals, time.Now())
	require.NoError(t, err)

	ev, err := NewMockDuplicateVoteEvidenceWithValidator(ctx, h, time.Now(), vals[0], "block-test-chain")
	require.NoError(t, err)
	evList := []Evidence{ev}

	block := MakeBlock(h, txs, extCommit.ToCommit(), evList)
	require.NotNil(t, block)
	require.Equal(t, 1, len(block.Evidence))
	require.NotNil(t, block.EvidenceHash)
}

func TestBlockValidateBasic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.Error(t, (*Block)(nil).ValidateBasic())

	txs := []Tx{Tx("foo"), Tx("bar")}
	lastID := makeBlockIDRandom()
	h := int64(3)

	voteSet, valSet, vals := randVoteSet(ctx, t, h-1, 1, tmproto.PrecommitType, 10, 1)
	extCommit, err := makeExtCommit(ctx, lastID, h-1, 1, voteSet, vals, time.Now())
	require.NoError(t, err)
	commit := extCommit.ToCommit()

	ev, err := NewMockDuplicateVoteEvidenceWithValidator(ctx, h, time.Now(), vals[0], "block-test-chain")
	require.NoError(t, err)
	evList := []Evidence{ev}

	testCases := []struct {
		testName      string
		malleateBlock func(*Block)
		expErr        bool
	}{
		{"Make Block", func(blk *Block) {}, false},
		{"Make Block w/ proposer Addr", func(blk *Block) { blk.ProposerAddress = valSet.GetProposer().Address }, false},
		{"Negative Height", func(blk *Block) { blk.Height = -1 }, true},
		{"Remove 1/2 the commits", func(blk *Block) {
			blk.LastCommit.Signatures = commit.Signatures[:commit.Size()/2]
			blk.LastCommit.hash = nil // clear hash or change wont be noticed
		}, true},
		{"Remove LastCommitHash", func(blk *Block) { blk.LastCommitHash = []byte("something else") }, true},
		{"Tampered Data", func(blk *Block) {
			blk.Data.Txs[0] = Tx("something else")
			blk.Data.hash = nil // clear hash or change wont be noticed
		}, true},
		{"Tampered DataHash", func(blk *Block) {
			blk.DataHash = tmrand.Bytes(len(blk.DataHash))
		}, true},
		{"Tampered EvidenceHash", func(blk *Block) {
			blk.EvidenceHash = tmrand.Bytes(len(blk.EvidenceHash))
		}, true},
		{"Incorrect block protocol version", func(blk *Block) {
			blk.Version.Block = 1
		}, true},
		{"Missing LastCommit", func(blk *Block) {
			blk.LastCommit = nil
		}, true},
		{"Invalid LastCommit", func(blk *Block) {
			blk.LastCommit = &Commit{
				Height:  -1,
				BlockID: *voteSet.maj23,
			}
		}, true},
		{"Invalid Evidence", func(blk *Block) {
			emptyEv := &DuplicateVoteEvidence{}
			blk.Evidence = []Evidence{emptyEv}
		}, true},
	}
	for i, tc := range testCases {
		tc := tc
		i := i
		t.Run(tc.testName, func(t *testing.T) {
			block := MakeBlock(h, txs, commit, evList)
			block.ProposerAddress = valSet.GetProposer().Address
			tc.malleateBlock(block)
			err = block.ValidateBasic()
			t.Log(err)
			assert.Equal(t, tc.expErr, err != nil, "#%d: %v", i, err)
		})
	}
}

func TestBlockHash(t *testing.T) {
	assert.Nil(t, (*Block)(nil).Hash())
	assert.Nil(t, MakeBlock(int64(3), []Tx{Tx("Hello World")}, nil, nil).Hash())
}

func TestBlockMakePartSet(t *testing.T) {
	bps, err := (*Block)(nil).MakePartSet(2)
	assert.Error(t, err)
	assert.Nil(t, bps)

	partSet, err := MakeBlock(int64(3), []Tx{Tx("Hello World")}, nil, nil).MakePartSet(1024)
	require.NoError(t, err)
	assert.NotNil(t, partSet)
	assert.EqualValues(t, 1, partSet.Total())
}

func TestBlockMakePartSetWithEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bps, err := (*Block)(nil).MakePartSet(2)
	assert.Error(t, err)
	assert.Nil(t, bps)

	lastID := makeBlockIDRandom()
	h := int64(3)

	voteSet, _, vals := randVoteSet(ctx, t, h-1, 1, tmproto.PrecommitType, 10, 1)
	extCommit, err := makeExtCommit(ctx, lastID, h-1, 1, voteSet, vals, time.Now())
	require.NoError(t, err)

	ev, err := NewMockDuplicateVoteEvidenceWithValidator(ctx, h, time.Now(), vals[0], "block-test-chain")
	require.NoError(t, err)
	evList := []Evidence{ev}

	partSet, err := MakeBlock(h, []Tx{Tx("Hello World")}, extCommit.ToCommit(), evList).MakePartSet(512)
	require.NoError(t, err)

	assert.NotNil(t, partSet)
	assert.EqualValues(t, 4, partSet.Total())
}

func TestBlockHashesTo(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	assert.False(t, (*Block)(nil).HashesTo(nil))

	lastID := makeBlockIDRandom()
	h := int64(3)

	voteSet, valSet, vals := randVoteSet(ctx, t, h-1, 1, tmproto.PrecommitType, 10, 1)
	extCommit, err := makeExtCommit(ctx, lastID, h-1, 1, voteSet, vals, time.Now())
	require.NoError(t, err)

	ev, err := NewMockDuplicateVoteEvidenceWithValidator(ctx, h, time.Now(), vals[0], "block-test-chain")
	require.NoError(t, err)
	evList := []Evidence{ev}

	block := MakeBlock(h, []Tx{Tx("Hello World")}, extCommit.ToCommit(), evList)
	block.ValidatorsHash = valSet.Hash()
	assert.False(t, block.HashesTo([]byte{}))
	assert.False(t, block.HashesTo([]byte("something else")))
	assert.True(t, block.HashesTo(block.Hash()))
}

func TestBlockSize(t *testing.T) {
	size := MakeBlock(int64(3), []Tx{Tx("Hello World")}, nil, nil).Size()
	if size <= 0 {
		t.Fatal("Size of the block is zero or negative")
	}
}

func TestBlockString(t *testing.T) {
	assert.Equal(t, "nil-Block", (*Block)(nil).String())
	assert.Equal(t, "nil-Block", (*Block)(nil).StringIndented(""))
	assert.Equal(t, "nil-Block", (*Block)(nil).StringShort())

	block := MakeBlock(int64(3), []Tx{Tx("Hello World")}, nil, nil)
	assert.NotEqual(t, "nil-Block", block.String())
	assert.NotEqual(t, "nil-Block", block.StringIndented(""))
	assert.NotEqual(t, "nil-Block", block.StringShort())
}

func makeBlockIDRandom() BlockID {
	var (
		blockHash   = make([]byte, crypto.HashSize)
		partSetHash = make([]byte, crypto.HashSize)
	)
	rand.Read(blockHash)   //nolint: errcheck // ignore errcheck for read
	rand.Read(partSetHash) //nolint: errcheck // ignore errcheck for read
	return BlockID{blockHash, PartSetHeader{123, partSetHash}}
}

func makeBlockID(hash []byte, partSetSize uint32, partSetHash []byte) BlockID {
	var (
		h   = make([]byte, crypto.HashSize)
		psH = make([]byte, crypto.HashSize)
	)
	copy(h, hash)
	copy(psH, partSetHash)
	return BlockID{
		Hash: h,
		PartSetHeader: PartSetHeader{
			Total: partSetSize,
			Hash:  psH,
		},
	}
}

var nilBytes []byte

// This follows RFC-6962, i.e. `echo -n ” | sha256sum`
var emptyBytes = []byte{0xe3, 0xb0, 0xc4, 0x42, 0x98, 0xfc, 0x1c, 0x14, 0x9a, 0xfb, 0xf4, 0xc8,
	0x99, 0x6f, 0xb9, 0x24, 0x27, 0xae, 0x41, 0xe4, 0x64, 0x9b, 0x93, 0x4c, 0xa4, 0x95, 0x99, 0x1b,
	0x78, 0x52, 0xb8, 0x55}

func TestNilHeaderHashDoesntCrash(t *testing.T) {
	assert.Equal(t, nilBytes, []byte((*Header)(nil).Hash()))
	assert.Equal(t, nilBytes, []byte((new(Header)).Hash()))
}

func TestNilDataHashDoesntCrash(t *testing.T) {
	assert.Equal(t, emptyBytes, []byte((*Data)(nil).Hash(false)))
	assert.Equal(t, emptyBytes, []byte(new(Data).Hash(false)))
}

func TestCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lastID := makeBlockIDRandom()
	h := int64(3)
	voteSet, _, vals := randVoteSet(ctx, t, h-1, 1, tmproto.PrecommitType, 10, 1)
	commit, err := makeExtCommit(ctx, lastID, h-1, 1, voteSet, vals, time.Now())
	require.NoError(t, err)

	assert.Equal(t, h-1, commit.Height)
	assert.EqualValues(t, 1, commit.Round)
	assert.Equal(t, tmproto.PrecommitType, tmproto.SignedMsgType(commit.Type()))
	if commit.Size() <= 0 {
		t.Fatalf("commit %v has a zero or negative size: %d", commit, commit.Size())
	}

	require.NotNil(t, commit.BitArray())
	assert.Equal(t, bits.NewBitArray(10).Size(), commit.BitArray().Size())

	assert.Equal(t, voteSet.GetByIndex(0), commit.GetByIndex(0))
	assert.True(t, commit.IsCommit())
}

func TestCommitValidateBasic(t *testing.T) {
	testCases := []struct {
		testName       string
		malleateCommit func(*Commit)
		expectErr      bool
	}{
		{"Random Commit", func(com *Commit) {}, false},
		{"Incorrect signature", func(com *Commit) { com.Signatures[0].Signature = []byte{0} }, false},
		{"Incorrect height", func(com *Commit) { com.Height = int64(-100) }, true},
		{"Incorrect round", func(com *Commit) { com.Round = -100 }, true},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.testName, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			com := randCommit(ctx, t, time.Now())

			tc.malleateCommit(com)
			assert.Equal(t, tc.expectErr, com.ValidateBasic() != nil, "Validate Basic had an unexpected result")
		})
	}
}

func TestMaxCommitBytes(t *testing.T) {
	// time is varint encoded so need to pick the max.
	// year int, month Month, day, hour, min, sec, nsec int, loc *Location
	timestamp := time.Date(math.MaxInt64, 0, 0, 0, 0, 0, math.MaxInt64, time.UTC)

	cs := CommitSig{
		BlockIDFlag:      BlockIDFlagNil,
		ValidatorAddress: crypto.AddressHash([]byte("validator_address")),
		Timestamp:        timestamp,
		Signature:        crypto.CRandBytes(MaxSignatureSize),
	}

	pbSig := cs.ToProto()
	// test that a single commit sig doesn't exceed max commit sig bytes
	assert.EqualValues(t, MaxCommitSigBytes, pbSig.Size())

	// check size with a single commit
	commit := &Commit{
		Height: math.MaxInt64,
		Round:  math.MaxInt32,
		BlockID: BlockID{
			Hash: crypto.Checksum([]byte("blockID_hash")),
			PartSetHeader: PartSetHeader{
				Total: math.MaxInt32,
				Hash:  crypto.Checksum([]byte("blockID_part_set_header_hash")),
			},
		},
		Signatures: []CommitSig{cs},
	}

	pb := commit.ToProto()

	assert.EqualValues(t, MaxCommitBytes(1), int64(pb.Size()))

	// check the upper bound of the commit size
	for i := 1; i < MaxVotesCount; i++ {
		commit.Signatures = append(commit.Signatures, cs)
	}

	pb = commit.ToProto()

	assert.EqualValues(t, MaxCommitBytes(MaxVotesCount), int64(pb.Size()))

}

func TestHeaderHash(t *testing.T) {
	testCases := []struct {
		desc       string
		header     *Header
		expectHash bytes.HexBytes
	}{
		{"Generates expected hash", &Header{
			Version:            version.Consensus{Block: 1, App: 2},
			ChainID:            "chainId",
			Height:             3,
			Time:               time.Date(2019, 10, 13, 16, 14, 44, 0, time.UTC),
			LastBlockID:        makeBlockID(make([]byte, crypto.HashSize), 6, make([]byte, crypto.HashSize)),
			LastCommitHash:     crypto.Checksum([]byte("last_commit_hash")),
			DataHash:           crypto.Checksum([]byte("data_hash")),
			ValidatorsHash:     crypto.Checksum([]byte("validators_hash")),
			NextValidatorsHash: crypto.Checksum([]byte("next_validators_hash")),
			ConsensusHash:      crypto.Checksum([]byte("consensus_hash")),
			AppHash:            crypto.Checksum([]byte("app_hash")),
			LastResultsHash:    crypto.Checksum([]byte("last_results_hash")),
			EvidenceHash:       crypto.Checksum([]byte("evidence_hash")),
			ProposerAddress:    crypto.AddressHash([]byte("proposer_address")),
		}, hexBytesFromString(t, "F740121F553B5418C3EFBD343C2DBFE9E007BB67B0D020A0741374BAB65242A4")},
		{"nil header yields nil", nil, nil},
		{"nil ValidatorsHash yields nil", &Header{
			Version:            version.Consensus{Block: 1, App: 2},
			ChainID:            "chainId",
			Height:             3,
			Time:               time.Date(2019, 10, 13, 16, 14, 44, 0, time.UTC),
			LastBlockID:        makeBlockID(make([]byte, crypto.HashSize), 6, make([]byte, crypto.HashSize)),
			LastCommitHash:     crypto.Checksum([]byte("last_commit_hash")),
			DataHash:           crypto.Checksum([]byte("data_hash")),
			ValidatorsHash:     nil,
			NextValidatorsHash: crypto.Checksum([]byte("next_validators_hash")),
			ConsensusHash:      crypto.Checksum([]byte("consensus_hash")),
			AppHash:            crypto.Checksum([]byte("app_hash")),
			LastResultsHash:    crypto.Checksum([]byte("last_results_hash")),
			EvidenceHash:       crypto.Checksum([]byte("evidence_hash")),
			ProposerAddress:    crypto.AddressHash([]byte("proposer_address")),
		}, nil},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			assert.Equal(t, tc.expectHash, tc.header.Hash())

			// We also make sure that all fields are hashed in struct order, and that all
			// fields in the test struct are non-zero.
			if tc.header != nil && tc.expectHash != nil {
				byteSlices := [][]byte{}

				s := reflect.ValueOf(*tc.header)
				for i := 0; i < s.NumField(); i++ {
					f := s.Field(i)

					assert.False(t, f.IsZero(), "Found zero-valued field %v",
						s.Type().Field(i).Name)

					switch f := f.Interface().(type) {
					case int64, bytes.HexBytes, string:
						byteSlices = append(byteSlices, cdcEncode(f))
					case time.Time:
						bz, err := gogotypes.StdTimeMarshal(f)
						require.NoError(t, err)
						byteSlices = append(byteSlices, bz)
					case version.Consensus:
						pbc := tmversion.Consensus{
							Block: f.Block,
							App:   f.App,
						}
						bz, err := pbc.Marshal()
						require.NoError(t, err)
						byteSlices = append(byteSlices, bz)
					case BlockID:
						pbbi := f.ToProto()
						bz, err := pbbi.Marshal()
						require.NoError(t, err)
						byteSlices = append(byteSlices, bz)
					default:
						t.Errorf("unknown type %T", f)
					}
				}
				assert.Equal(t,
					bytes.HexBytes(merkle.HashFromByteSlices(byteSlices)), tc.header.Hash())
			}
		})
	}
}

func TestMaxHeaderBytes(t *testing.T) {
	// Construct a UTF-8 string of MaxChainIDLen length using the supplementary
	// characters.
	// Each supplementary character takes 4 bytes.
	// http://www.i18nguy.com/unicode/supplementary-test.html
	maxChainID := ""
	for i := 0; i < MaxChainIDLen; i++ {
		maxChainID += "𠜎"
	}

	// time is varint encoded so need to pick the max.
	// year int, month Month, day, hour, min, sec, nsec int, loc *Location
	timestamp := time.Date(math.MaxInt64, 0, 0, 0, 0, 0, math.MaxInt64, time.UTC)

	h := Header{
		Version:            version.Consensus{Block: math.MaxInt64, App: math.MaxInt64},
		ChainID:            maxChainID,
		Height:             math.MaxInt64,
		Time:               timestamp,
		LastBlockID:        makeBlockID(make([]byte, crypto.HashSize), math.MaxInt32, make([]byte, crypto.HashSize)),
		LastCommitHash:     crypto.Checksum([]byte("last_commit_hash")),
		DataHash:           crypto.Checksum([]byte("data_hash")),
		ValidatorsHash:     crypto.Checksum([]byte("validators_hash")),
		NextValidatorsHash: crypto.Checksum([]byte("next_validators_hash")),
		ConsensusHash:      crypto.Checksum([]byte("consensus_hash")),
		AppHash:            crypto.Checksum([]byte("app_hash")),
		LastResultsHash:    crypto.Checksum([]byte("last_results_hash")),
		EvidenceHash:       crypto.Checksum([]byte("evidence_hash")),
		ProposerAddress:    crypto.AddressHash([]byte("proposer_address")),
	}

	bz, err := h.ToProto().Marshal()
	require.NoError(t, err)

	assert.EqualValues(t, MaxHeaderBytes, int64(len(bz)))
}

func randCommit(ctx context.Context, t *testing.T, now time.Time) *Commit {
	t.Helper()
	lastID := makeBlockIDRandom()
	h := int64(3)
	voteSet, _, vals := randVoteSet(ctx, t, h-1, 1, tmproto.PrecommitType, 10, 1)
	commit, err := makeExtCommit(ctx, lastID, h-1, 1, voteSet, vals, now)

	require.NoError(t, err)

	return commit.ToCommit()
}

func hexBytesFromString(t *testing.T, s string) bytes.HexBytes {
	t.Helper()

	b, err := hex.DecodeString(s)
	require.NoError(t, err)

	return bytes.HexBytes(b)
}

func TestBlockMaxDataBytes(t *testing.T) {
	testCases := []struct {
		maxBytes      int64
		valsCount     int
		evidenceBytes int64
		panics        bool
		result        int64
	}{
		0: {-10, 1, 0, true, 0},
		1: {10, 1, 0, true, 0},
		2: {841, 1, 0, true, 0},
		3: {842, 1, 0, false, 0},
		4: {843, 1, 0, false, 1},
		5: {954, 2, 0, false, 1},
		6: {1053, 2, 100, false, 0},
	}

	for i, tc := range testCases {
		tc := tc
		if tc.panics {
			assert.Panics(t, func() {
				MaxDataBytes(tc.maxBytes, tc.evidenceBytes, tc.valsCount)
			}, "#%v", i)
		} else {
			assert.Equal(t,
				tc.result,
				MaxDataBytes(tc.maxBytes, tc.evidenceBytes, tc.valsCount),
				"#%v", i)
		}
	}
}

func TestBlockMaxDataBytesNoEvidence(t *testing.T) {
	testCases := []struct {
		maxBytes  int64
		valsCount int
		panics    bool
		result    int64
	}{
		0: {-10, 1, true, 0},
		1: {10, 1, true, 0},
		2: {841, 1, true, 0},
		3: {842, 1, false, 0},
		4: {843, 1, false, 1},
	}

	for i, tc := range testCases {
		tc := tc
		if tc.panics {
			assert.Panics(t, func() {
				MaxDataBytesNoEvidence(tc.maxBytes, tc.valsCount)
			}, "#%v", i)
		} else {
			assert.Equal(t,
				tc.result,
				MaxDataBytesNoEvidence(tc.maxBytes, tc.valsCount),
				"#%v", i)
		}
	}
}

// TestVoteSetToExtendedCommit tests that the extended commit produced from a
// vote set contains the same vote information as the vote set. The test ensures
// that the MakeExtendedCommit method behaves as expected, whether vote extensions
// are present in the original votes or not.
func TestVoteSetToExtendedCommit(t *testing.T) {
	for _, testCase := range []struct {
		name             string
		includeExtension bool
	}{
		{
			name:             "no extensions",
			includeExtension: false,
		},
		{
			name:             "with extensions",
			includeExtension: true,
		},
	} {

		t.Run(testCase.name, func(t *testing.T) {
			blockID := makeBlockIDRandom()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			valSet, vals := randValidatorPrivValSet(ctx, t, 10, 1)
			var voteSet *VoteSet
			if testCase.includeExtension {
				voteSet = NewExtendedVoteSet("test_chain_id", 3, 1, tmproto.PrecommitType, valSet)
			} else {
				voteSet = NewVoteSet("test_chain_id", 3, 1, tmproto.PrecommitType, valSet)
			}
			for i := 0; i < len(vals); i++ {
				pubKey, err := vals[i].GetPubKey(ctx)
				require.NoError(t, err)
				vote := &Vote{
					ValidatorAddress: pubKey.Address(),
					ValidatorIndex:   int32(i),
					Height:           3,
					Round:            1,
					Type:             tmproto.PrecommitType,
					BlockID:          blockID,
					Timestamp:        time.Now(),
				}
				v := vote.ToProto()
				err = vals[i].SignVote(ctx, voteSet.ChainID(), v)
				require.NoError(t, err)
				vote.Signature = v.Signature
				if testCase.includeExtension {
					vote.ExtensionSignature = v.ExtensionSignature
				}
				added, err := voteSet.AddVote(vote)
				require.NoError(t, err)
				require.True(t, added)
			}
			ec := voteSet.MakeExtendedCommit()

			for i := int32(0); int(i) < len(vals); i++ {
				vote1 := voteSet.GetByIndex(i)
				vote2 := ec.GetExtendedVote(i)

				vote1bz, err := vote1.ToProto().Marshal()
				require.NoError(t, err)
				vote2bz, err := vote2.ToProto().Marshal()
				require.NoError(t, err)
				assert.Equal(t, vote1bz, vote2bz)
			}
		})
	}
}

// TestExtendedCommitToVoteSet tests that the vote set produced from an extended commit
// contains the same vote information as the extended commit. The test ensures
// that the ToVoteSet method behaves as expected, whether vote extensions
// are present in the original votes or not.
func TestExtendedCommitToVoteSet(t *testing.T) {
	for _, testCase := range []struct {
		name             string
		includeExtension bool
	}{
		{
			name:             "no extensions",
			includeExtension: false,
		},
		{
			name:             "with extensions",
			includeExtension: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			lastID := makeBlockIDRandom()
			h := int64(3)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			voteSet, valSet, vals := randVoteSet(ctx, t, h-1, 1, tmproto.PrecommitType, 10, 1)
			extCommit, err := makeExtCommit(ctx, lastID, h-1, 1, voteSet, vals, time.Now())
			assert.NoError(t, err)

			if !testCase.includeExtension {
				for i := 0; i < len(vals); i++ {
					v := voteSet.GetByIndex(int32(i))
					v.Extension = nil
					v.ExtensionSignature = nil
					extCommit.ExtendedSignatures[i].Extension = nil
					extCommit.ExtendedSignatures[i].ExtensionSignature = nil
				}
			}

			chainID := voteSet.ChainID()
			var voteSet2 *VoteSet
			if testCase.includeExtension {
				voteSet2 = extCommit.ToExtendedVoteSet(chainID, valSet)
			} else {
				voteSet2 = extCommit.ToVoteSet(chainID, valSet)
			}

			for i := int32(0); int(i) < len(vals); i++ {
				vote1 := voteSet.GetByIndex(i)
				vote2 := voteSet2.GetByIndex(i)
				vote3 := extCommit.GetExtendedVote(i)

				vote1bz, err := vote1.ToProto().Marshal()
				require.NoError(t, err)
				vote2bz, err := vote2.ToProto().Marshal()
				require.NoError(t, err)
				vote3bz, err := vote3.ToProto().Marshal()
				require.NoError(t, err)
				assert.Equal(t, vote1bz, vote2bz)
				assert.Equal(t, vote1bz, vote3bz)
			}
		})
	}
}

func TestCommitToVoteSetWithVotesForNilBlock(t *testing.T) {
	blockID := makeBlockID([]byte("blockhash"), 1000, []byte("partshash"))

	const (
		height = int64(3)
		round  = 0
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type commitVoteTest struct {
		blockIDs      []BlockID
		numVotes      []int // must sum to numValidators
		numValidators int
		valid         bool
	}

	testCases := []commitVoteTest{
		{[]BlockID{blockID, {}}, []int{67, 33}, 100, true},
	}

	for _, tc := range testCases {
		voteSet, valSet, vals := randVoteSet(ctx, t, height-1, round, tmproto.PrecommitType, tc.numValidators, 1)

		vi := int32(0)
		for n := range tc.blockIDs {
			for i := 0; i < tc.numVotes[n]; i++ {
				pubKey, err := vals[vi].GetPubKey(ctx)
				require.NoError(t, err)
				vote := &Vote{
					ValidatorAddress: pubKey.Address(),
					ValidatorIndex:   vi,
					Height:           height - 1,
					Round:            round,
					Type:             tmproto.PrecommitType,
					BlockID:          tc.blockIDs[n],
					Timestamp:        tmtime.Now(),
				}

				added, err := signAddVote(ctx, vals[vi], vote, voteSet)
				assert.NoError(t, err)
				assert.True(t, added)

				vi++
			}
		}

		if tc.valid {
			extCommit := voteSet.MakeExtendedCommit() // panics without > 2/3 valid votes
			assert.NotNil(t, extCommit)
			err := valSet.VerifyCommit(voteSet.ChainID(), blockID, height-1, extCommit.ToCommit())
			assert.NoError(t, err)
		} else {
			assert.Panics(t, func() { voteSet.MakeExtendedCommit() })
		}
	}
}

func TestBlockIDValidateBasic(t *testing.T) {
	validBlockID := BlockID{
		Hash: bytes.HexBytes{},
		PartSetHeader: PartSetHeader{
			Total: 1,
			Hash:  bytes.HexBytes{},
		},
	}

	invalidBlockID := BlockID{
		Hash: []byte{0},
		PartSetHeader: PartSetHeader{
			Total: 1,
			Hash:  []byte{0},
		},
	}

	testCases := []struct {
		testName             string
		blockIDHash          bytes.HexBytes
		blockIDPartSetHeader PartSetHeader
		expectErr            bool
	}{
		{"Valid BlockID", validBlockID.Hash, validBlockID.PartSetHeader, false},
		{"Invalid BlockID", invalidBlockID.Hash, validBlockID.PartSetHeader, true},
		{"Invalid BlockID", validBlockID.Hash, invalidBlockID.PartSetHeader, true},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.testName, func(t *testing.T) {
			blockID := BlockID{
				Hash:          tc.blockIDHash,
				PartSetHeader: tc.blockIDPartSetHeader,
			}
			assert.Equal(t, tc.expectErr, blockID.ValidateBasic() != nil, "Validate Basic had an unexpected result")
		})
	}
}

func TestBlockProtoBuf(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := mrand.Int63()
	c1 := randCommit(ctx, t, time.Now())

	b1 := MakeBlock(h, []Tx{Tx([]byte{1})}, &Commit{Signatures: []CommitSig{}}, []Evidence{})
	b1.ProposerAddress = tmrand.Bytes(crypto.AddressSize)

	b2 := MakeBlock(h, []Tx{Tx([]byte{1})}, c1, []Evidence{})
	b2.ProposerAddress = tmrand.Bytes(crypto.AddressSize)
	evidenceTime := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	evi, err := NewMockDuplicateVoteEvidence(ctx, h, evidenceTime, "block-test-chain")
	require.NoError(t, err)
	b2.Evidence = EvidenceList{evi}
	b2.EvidenceHash = b2.Evidence.Hash()

	b3 := MakeBlock(h, []Tx{}, c1, []Evidence{})
	b3.ProposerAddress = tmrand.Bytes(crypto.AddressSize)
	testCases := []struct {
		msg      string
		b1       *Block
		expPass  bool
		expPass2 bool
	}{
		{"nil block", nil, false, false},
		{"b1", b1, true, true},
		{"b2", b2, true, true},
		{"b3", b3, true, true},
	}
	for _, tc := range testCases {
		pb, err := tc.b1.ToProto()
		if tc.expPass {
			require.NoError(t, err, tc.msg)
		} else {
			require.Error(t, err, tc.msg)
		}

		block, err := BlockFromProto(pb)
		if tc.expPass2 {
			require.NoError(t, err, tc.msg)
			require.EqualValues(t, tc.b1.Header, block.Header, tc.msg)
			require.EqualValues(t, tc.b1.Data, block.Data, tc.msg)
			require.EqualValues(t, tc.b1.Evidence, block.Evidence, tc.msg)
			require.EqualValues(t, *tc.b1.LastCommit, *block.LastCommit, tc.msg)
		} else {
			require.Error(t, err, tc.msg)
		}
	}
}

func TestDataProtoBuf(t *testing.T) {
	data := &Data{Txs: Txs{Tx([]byte{1}), Tx([]byte{2}), Tx([]byte{3})}}
	data2 := &Data{Txs: Txs{}}
	testCases := []struct {
		msg     string
		data1   *Data
		expPass bool
	}{
		{"success", data, true},
		{"success data2", data2, true},
	}
	for _, tc := range testCases {
		protoData := tc.data1.ToProto()
		d, err := DataFromProto(&protoData)
		if tc.expPass {
			require.NoError(t, err, tc.msg)
			require.EqualValues(t, tc.data1, &d, tc.msg)
		} else {
			require.Error(t, err, tc.msg)
		}
	}
}

// exposed for testing
func MakeRandHeader() Header {
	chainID := "test"
	t := time.Now()
	height := mrand.Int63()
	randBytes := tmrand.Bytes(crypto.HashSize)
	randAddress := tmrand.Bytes(crypto.AddressSize)
	h := Header{
		Version:            version.Consensus{Block: version.BlockProtocol, App: 1},
		ChainID:            chainID,
		Height:             height,
		Time:               t,
		LastBlockID:        BlockID{},
		LastCommitHash:     randBytes,
		DataHash:           randBytes,
		ValidatorsHash:     randBytes,
		NextValidatorsHash: randBytes,
		ConsensusHash:      randBytes,
		AppHash:            randBytes,

		LastResultsHash: randBytes,

		EvidenceHash:    randBytes,
		ProposerAddress: randAddress,
	}

	return h
}

func TestHeaderProto(t *testing.T) {
	h1 := MakeRandHeader()
	tc := []struct {
		msg     string
		h1      *Header
		expPass bool
	}{
		{"success", &h1, true},
		{"failure empty Header", &Header{}, false},
	}

	for _, tt := range tc {
		tt := tt
		t.Run(tt.msg, func(t *testing.T) {
			pb := tt.h1.ToProto()
			h, err := HeaderFromProto(pb)
			if tt.expPass {
				require.NoError(t, err, tt.msg)
				require.Equal(t, tt.h1, &h, tt.msg)
			} else {
				require.Error(t, err, tt.msg)
			}

		})
	}
}

func TestBlockIDProtoBuf(t *testing.T) {
	blockID := makeBlockID([]byte("hash"), 2, []byte("part_set_hash"))
	testCases := []struct {
		msg     string
		bid1    *BlockID
		expPass bool
	}{
		{"success", &blockID, true},
		{"success empty", &BlockID{}, true},
		{"failure BlockID nil", nil, false},
	}
	for _, tc := range testCases {
		protoBlockID := tc.bid1.ToProto()

		bi, err := BlockIDFromProto(&protoBlockID)
		if tc.expPass {
			require.NoError(t, err)
			require.Equal(t, tc.bid1, bi, tc.msg)
		} else {
			require.NotEqual(t, tc.bid1, bi, tc.msg)
		}
	}
}

func TestSignedHeaderProtoBuf(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	commit := randCommit(ctx, t, time.Now())

	h := MakeRandHeader()

	sh := SignedHeader{Header: &h, Commit: commit}

	testCases := []struct {
		msg     string
		sh1     *SignedHeader
		expPass bool
	}{
		{"empty SignedHeader 2", &SignedHeader{}, true},
		{"success", &sh, true},
		{"failure nil", nil, false},
	}
	for _, tc := range testCases {
		protoSignedHeader := tc.sh1.ToProto()

		sh, err := SignedHeaderFromProto(protoSignedHeader)

		if tc.expPass {
			require.NoError(t, err, tc.msg)
			require.Equal(t, tc.sh1, sh, tc.msg)
		} else {
			require.Error(t, err, tc.msg)
		}
	}
}

func TestBlockIDEquals(t *testing.T) {
	var (
		blockID          = makeBlockID([]byte("hash"), 2, []byte("part_set_hash"))
		blockIDDuplicate = makeBlockID([]byte("hash"), 2, []byte("part_set_hash"))
		blockIDDifferent = makeBlockID([]byte("different_hash"), 2, []byte("part_set_hash"))
		blockIDEmpty     = BlockID{}
	)

	assert.True(t, blockID.Equals(blockIDDuplicate))
	assert.False(t, blockID.Equals(blockIDDifferent))
	assert.False(t, blockID.Equals(blockIDEmpty))
	assert.True(t, blockIDEmpty.Equals(blockIDEmpty))
	assert.False(t, blockIDEmpty.Equals(blockIDDifferent))
}

func TestCommitSig_ValidateBasic(t *testing.T) {
	testCases := []struct {
		name      string
		cs        CommitSig
		expectErr bool
		errString string
	}{
		{
			"invalid ID flag",
			CommitSig{BlockIDFlag: BlockIDFlag(0xFF)},
			true, "unknown BlockIDFlag",
		},
		{
			"BlockIDFlagAbsent validator address present",
			CommitSig{BlockIDFlag: BlockIDFlagAbsent, ValidatorAddress: crypto.Address("testaddr")},
			true, "validator address is present",
		},
		{
			"BlockIDFlagAbsent timestamp present",
			CommitSig{BlockIDFlag: BlockIDFlagAbsent, Timestamp: time.Now().UTC()},
			true, "time is present",
		},
		{
			"BlockIDFlagAbsent signatures present",
			CommitSig{BlockIDFlag: BlockIDFlagAbsent, Signature: []byte{0xAA}},
			true, "signature is present",
		},
		{
			"BlockIDFlagAbsent valid BlockIDFlagAbsent",
			CommitSig{BlockIDFlag: BlockIDFlagAbsent},
			false, "",
		},
		{
			"non-BlockIDFlagAbsent invalid validator address",
			CommitSig{BlockIDFlag: BlockIDFlagCommit, ValidatorAddress: make([]byte, 1)},
			true, "expected ValidatorAddress size",
		},
		{
			"non-BlockIDFlagAbsent invalid signature (zero)",
			CommitSig{
				BlockIDFlag:      BlockIDFlagCommit,
				ValidatorAddress: make([]byte, crypto.AddressSize),
				Signature:        make([]byte, 0),
			},
			true, "signature is missing",
		},
		{
			"non-BlockIDFlagAbsent invalid signature (too large)",
			CommitSig{
				BlockIDFlag:      BlockIDFlagCommit,
				ValidatorAddress: make([]byte, crypto.AddressSize),
				Signature:        make([]byte, MaxSignatureSize+1),
			},
			true, "signature is too big",
		},
		{
			"non-BlockIDFlagAbsent valid",
			CommitSig{
				BlockIDFlag:      BlockIDFlagCommit,
				ValidatorAddress: make([]byte, crypto.AddressSize),
				Signature:        make([]byte, MaxSignatureSize),
			},
			false, "",
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			err := tc.cs.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errString)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHeader_ValidateBasic(t *testing.T) {
	testCases := []struct {
		name      string
		header    Header
		expectErr bool
		errString string
	}{
		{
			"invalid version block",
			Header{Version: version.Consensus{Block: version.BlockProtocol + 1}},
			true, "block protocol is incorrect",
		},
		{
			"invalid chain ID length",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen+1)),
			},
			true, "chainID is too long",
		},
		{
			"invalid height (negative)",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen)),
				Height:  -1,
			},
			true, "negative Height",
		},
		{
			"invalid height (zero)",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen)),
				Height:  0,
			},
			true, "zero Height",
		},
		{
			"invalid block ID hash",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen)),
				Height:  1,
				LastBlockID: BlockID{
					Hash: make([]byte, crypto.HashSize+1),
				},
			},
			true, "wrong Hash",
		},
		{
			"invalid block ID parts header hash",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen)),
				Height:  1,
				LastBlockID: BlockID{
					Hash: make([]byte, crypto.HashSize),
					PartSetHeader: PartSetHeader{
						Hash: make([]byte, crypto.HashSize+1),
					},
				},
			},
			true, "wrong PartSetHeader",
		},
		{
			"invalid last commit hash",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen)),
				Height:  1,
				LastBlockID: BlockID{
					Hash: make([]byte, crypto.HashSize),
					PartSetHeader: PartSetHeader{
						Hash: make([]byte, crypto.HashSize),
					},
				},
				LastCommitHash: make([]byte, crypto.HashSize+1),
			},
			true, "wrong LastCommitHash",
		},
		{
			"invalid data hash",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen)),
				Height:  1,
				LastBlockID: BlockID{
					Hash: make([]byte, crypto.HashSize),
					PartSetHeader: PartSetHeader{
						Hash: make([]byte, crypto.HashSize),
					},
				},
				LastCommitHash: make([]byte, crypto.HashSize),
				DataHash:       make([]byte, crypto.HashSize+1),
			},
			true, "wrong DataHash",
		},
		{
			"invalid evidence hash",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen)),
				Height:  1,
				LastBlockID: BlockID{
					Hash: make([]byte, crypto.HashSize),
					PartSetHeader: PartSetHeader{
						Hash: make([]byte, crypto.HashSize),
					},
				},
				LastCommitHash: make([]byte, crypto.HashSize),
				DataHash:       make([]byte, crypto.HashSize),
				EvidenceHash:   make([]byte, crypto.HashSize+1),
			},
			true, "wrong EvidenceHash",
		},
		{
			"invalid proposer address",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen)),
				Height:  1,
				LastBlockID: BlockID{
					Hash: make([]byte, crypto.HashSize),
					PartSetHeader: PartSetHeader{
						Hash: make([]byte, crypto.HashSize),
					},
				},
				LastCommitHash:  make([]byte, crypto.HashSize),
				DataHash:        make([]byte, crypto.HashSize),
				EvidenceHash:    make([]byte, crypto.HashSize),
				ProposerAddress: make([]byte, crypto.AddressSize+1),
			},
			true, "invalid ProposerAddress length",
		},
		{
			"invalid validator hash",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen)),
				Height:  1,
				LastBlockID: BlockID{
					Hash: make([]byte, crypto.HashSize),
					PartSetHeader: PartSetHeader{
						Hash: make([]byte, crypto.HashSize),
					},
				},
				LastCommitHash:  make([]byte, crypto.HashSize),
				DataHash:        make([]byte, crypto.HashSize),
				EvidenceHash:    make([]byte, crypto.HashSize),
				ProposerAddress: make([]byte, crypto.AddressSize),
				ValidatorsHash:  make([]byte, crypto.HashSize+1),
			},
			true, "wrong ValidatorsHash",
		},
		{
			"invalid next validator hash",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen)),
				Height:  1,
				LastBlockID: BlockID{
					Hash: make([]byte, crypto.HashSize),
					PartSetHeader: PartSetHeader{
						Hash: make([]byte, crypto.HashSize),
					},
				},
				LastCommitHash:     make([]byte, crypto.HashSize),
				DataHash:           make([]byte, crypto.HashSize),
				EvidenceHash:       make([]byte, crypto.HashSize),
				ProposerAddress:    make([]byte, crypto.AddressSize),
				ValidatorsHash:     make([]byte, crypto.HashSize),
				NextValidatorsHash: make([]byte, crypto.HashSize+1),
			},
			true, "wrong NextValidatorsHash",
		},
		{
			"invalid consensus hash",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen)),
				Height:  1,
				LastBlockID: BlockID{
					Hash: make([]byte, crypto.HashSize),
					PartSetHeader: PartSetHeader{
						Hash: make([]byte, crypto.HashSize),
					},
				},
				LastCommitHash:     make([]byte, crypto.HashSize),
				DataHash:           make([]byte, crypto.HashSize),
				EvidenceHash:       make([]byte, crypto.HashSize),
				ProposerAddress:    make([]byte, crypto.AddressSize),
				ValidatorsHash:     make([]byte, crypto.HashSize),
				NextValidatorsHash: make([]byte, crypto.HashSize),
				ConsensusHash:      make([]byte, crypto.HashSize+1),
			},
			true, "wrong ConsensusHash",
		},
		{
			"invalid last results hash",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen)),
				Height:  1,
				LastBlockID: BlockID{
					Hash: make([]byte, crypto.HashSize),
					PartSetHeader: PartSetHeader{
						Hash: make([]byte, crypto.HashSize),
					},
				},
				LastCommitHash:     make([]byte, crypto.HashSize),
				DataHash:           make([]byte, crypto.HashSize),
				EvidenceHash:       make([]byte, crypto.HashSize),
				ProposerAddress:    make([]byte, crypto.AddressSize),
				ValidatorsHash:     make([]byte, crypto.HashSize),
				NextValidatorsHash: make([]byte, crypto.HashSize),
				ConsensusHash:      make([]byte, crypto.HashSize),
				LastResultsHash:    make([]byte, crypto.HashSize+1),
			},
			true, "wrong LastResultsHash",
		},
		{
			"valid header",
			Header{
				Version: version.Consensus{Block: version.BlockProtocol},
				ChainID: string(make([]byte, MaxChainIDLen)),
				Height:  1,
				LastBlockID: BlockID{
					Hash: make([]byte, crypto.HashSize),
					PartSetHeader: PartSetHeader{
						Hash: make([]byte, crypto.HashSize),
					},
				},
				LastCommitHash:     make([]byte, crypto.HashSize),
				DataHash:           make([]byte, crypto.HashSize),
				EvidenceHash:       make([]byte, crypto.HashSize),
				ProposerAddress:    make([]byte, crypto.AddressSize),
				ValidatorsHash:     make([]byte, crypto.HashSize),
				NextValidatorsHash: make([]byte, crypto.HashSize),
				ConsensusHash:      make([]byte, crypto.HashSize),
				LastResultsHash:    make([]byte, crypto.HashSize),
			},
			false, "",
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			err := tc.header.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errString)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCommit_ValidateBasic(t *testing.T) {
	testCases := []struct {
		name      string
		commit    *Commit
		expectErr bool
		errString string
	}{
		{
			"invalid height",
			&Commit{Height: -1},
			true, "negative Height",
		},
		{
			"invalid round",
			&Commit{Height: 1, Round: -1},
			true, "negative Round",
		},
		{
			"invalid block ID",
			&Commit{
				Height:  1,
				Round:   1,
				BlockID: BlockID{},
			},
			true, "commit cannot be for nil block",
		},
		{
			"no signatures",
			&Commit{
				Height: 1,
				Round:  1,
				BlockID: BlockID{
					Hash: make([]byte, crypto.HashSize),
					PartSetHeader: PartSetHeader{
						Hash: make([]byte, crypto.HashSize),
					},
				},
			},
			true, "no signatures in commit",
		},
		{
			"invalid signature",
			&Commit{
				Height: 1,
				Round:  1,
				BlockID: BlockID{
					Hash: make([]byte, crypto.HashSize),
					PartSetHeader: PartSetHeader{
						Hash: make([]byte, crypto.HashSize),
					},
				},
				Signatures: []CommitSig{
					{
						BlockIDFlag:      BlockIDFlagCommit,
						ValidatorAddress: make([]byte, crypto.AddressSize),
						Signature:        make([]byte, MaxSignatureSize+1),
					},
				},
			},
			true, "wrong CommitSig",
		},
		{
			"valid commit",
			&Commit{
				Height: 1,
				Round:  1,
				BlockID: BlockID{
					Hash: make([]byte, crypto.HashSize),
					PartSetHeader: PartSetHeader{
						Hash: make([]byte, crypto.HashSize),
					},
				},
				Signatures: []CommitSig{
					{
						BlockIDFlag:      BlockIDFlagCommit,
						ValidatorAddress: make([]byte, crypto.AddressSize),
						Signature:        make([]byte, MaxSignatureSize),
					},
				},
			},
			false, "",
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			err := tc.commit.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errString)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHeaderHashVector(t *testing.T) {
	chainID := "test"
	h := Header{
		Version:            version.Consensus{Block: 1, App: 1},
		ChainID:            chainID,
		Height:             50,
		Time:               time.Date(math.MaxInt64, 0, 0, 0, 0, 0, math.MaxInt64, time.UTC),
		LastBlockID:        BlockID{},
		LastCommitHash:     []byte("f2564c78071e26643ae9b3e2a19fa0dc10d4d9e873aa0be808660123f11a1e78"),
		DataHash:           []byte("f2564c78071e26643ae9b3e2a19fa0dc10d4d9e873aa0be808660123f11a1e78"),
		ValidatorsHash:     []byte("f2564c78071e26643ae9b3e2a19fa0dc10d4d9e873aa0be808660123f11a1e78"),
		NextValidatorsHash: []byte("f2564c78071e26643ae9b3e2a19fa0dc10d4d9e873aa0be808660123f11a1e78"),
		ConsensusHash:      []byte("f2564c78071e26643ae9b3e2a19fa0dc10d4d9e873aa0be808660123f11a1e78"),
		AppHash:            []byte("f2564c78071e26643ae9b3e2a19fa0dc10d4d9e873aa0be808660123f11a1e78"),

		LastResultsHash: []byte("f2564c78071e26643ae9b3e2a19fa0dc10d4d9e873aa0be808660123f11a1e78"),

		EvidenceHash:    []byte("f2564c78071e26643ae9b3e2a19fa0dc10d4d9e873aa0be808660123f11a1e78"),
		ProposerAddress: []byte("2915b7b15f979e48ebc61774bb1d86ba3136b7eb"),
	}

	testCases := []struct {
		header   Header
		expBytes string
	}{
		{header: h, expBytes: "87b6117ac7f827d656f178a3d6d30b24b205db2b6a3a053bae8baf4618570bfc"},
	}

	for _, tc := range testCases {
		hash := tc.header.Hash()
		require.Equal(t, tc.expBytes, hex.EncodeToString(hash))
	}
}
