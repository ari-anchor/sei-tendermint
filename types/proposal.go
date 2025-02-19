package types

import (
	"errors"
	"fmt"
	"math/bits"
	"time"

	"github.com/ari-anchor/sei-tendermint/internal/libs/protoio"
	tmbytes "github.com/ari-anchor/sei-tendermint/libs/bytes"
	tmtime "github.com/ari-anchor/sei-tendermint/libs/time"
	tmproto "github.com/ari-anchor/sei-tendermint/proto/tendermint/types"
)

var (
	ErrInvalidBlockPartSignature = errors.New("error invalid block part signature")
	ErrInvalidBlockPartHash      = errors.New("error invalid block part hash")
)

// Proposal defines a block proposal for the consensus.
// It refers to the block by BlockID field.
// It must be signed by the correct proposer for the given Height/Round
// to be considered valid. It may depend on votes from a previous round,
// a so-called Proof-of-Lock (POL) round, as noted in the POLRound.
// If POLRound >= 0, then BlockID corresponds to the block that is locked in POLRound.
type Proposal struct {
	Type            tmproto.SignedMsgType
	Height          int64     `json:"height,string"`
	Round           int32     `json:"round"`     // there can not be greater than 2_147_483_647 rounds
	POLRound        int32     `json:"pol_round"` // -1 if null.
	BlockID         BlockID   `json:"block_id"`
	Timestamp       time.Time `json:"timestamp"`
	Signature       []byte    `json:"signature"`
	TxKeys          []TxKey   `json:"tx_keys"`
	Header          `json:"header"`
	LastCommit      *Commit      `json:"last_commit"`
	Evidence        EvidenceList `json:"evidence"`
	ProposerAddress Address      `json:"proposer_address"` // original proposer of the block
}

// NewProposal returns a new Proposal.
// If there is no POLRound, polRound should be -1.
func NewProposal(height int64, round int32, polRound int32, blockID BlockID, ts time.Time, txKeys []TxKey, header Header, lastCommit *Commit, evidenceList EvidenceList, proposerAddress Address) *Proposal {
	return &Proposal{
		Type:            tmproto.ProposalType,
		Height:          height,
		Round:           round,
		BlockID:         blockID,
		POLRound:        polRound,
		Timestamp:       tmtime.Canonical(ts),
		TxKeys:          txKeys,
		Header:          header,
		LastCommit:      lastCommit,
		Evidence:        evidenceList,
		ProposerAddress: proposerAddress,
	}
}

// ValidateBasic performs basic validation.
func (p *Proposal) ValidateBasic() error {
	if p.Type != tmproto.ProposalType {
		return errors.New("invalid Type")
	}
	if p.Height < 0 {
		return errors.New("negative Height")
	}
	if p.Round < 0 {
		return errors.New("negative Round")
	}
	if p.POLRound < -1 {
		return errors.New("negative POLRound (exception: -1)")
	}
	if err := p.BlockID.ValidateBasic(); err != nil {
		return fmt.Errorf("wrong BlockID: %w", err)
	}
	// ValidateBasic above would pass even if the BlockID was empty:
	if !p.BlockID.IsComplete() {
		return fmt.Errorf("expected a complete, non-empty BlockID, got: %v", p.BlockID)
	}

	// NOTE: Timestamp validation is subtle and handled elsewhere.

	if len(p.Signature) == 0 {
		return errors.New("signature is missing")
	}

	if len(p.Signature) > MaxSignatureSize {
		return fmt.Errorf("signature is too big (max: %d)", MaxSignatureSize)
	}
	return nil
}

// IsTimely validates that the block timestamp is 'timely' according to the proposer-based timestamp algorithm.
// To evaluate if a block is timely, its timestamp is compared to the local time of the validator along with the
// configured Precision and MsgDelay parameters.
// Specifically, a proposed block timestamp is considered timely if it is satisfies the following inequalities:
//
// localtime >= proposedBlockTime - Precision
// localtime <= proposedBlockTime + MsgDelay + Precision
//
// For more information on the meaning of 'timely', see the proposer-based timestamp specification:
// https://github.com/ari-anchor/sei-tendermint/tree/master/spec/consensus/proposer-based-timestamp
func (p *Proposal) IsTimely(recvTime time.Time, sp SynchronyParams, round int32) bool {
	// The message delay values are scaled as rounds progress.
	// Every 10 rounds, the message delay is doubled to allow consensus to
	// proceed in the case that the chosen value was too small for the given network conditions.
	// For more information and discussion on this mechanism, see the relevant github issue:
	// https://github.com/tendermint/spec/issues/371
	maxShift := bits.LeadingZeros64(uint64(sp.MessageDelay)) - 1
	nShift := int((round / 10))

	if nShift > maxShift {
		// if the number of 'doublings' would would overflow the size of the int, use the
		// maximum instead.
		nShift = maxShift
	}
	msgDelay := sp.MessageDelay * time.Duration(1<<nShift)

	// lhs is `proposedBlockTime - Precision` in the first inequality
	lhs := p.Timestamp.Add(-sp.Precision)
	// rhs is `proposedBlockTime + MsgDelay + Precision` in the second inequality
	rhs := p.Timestamp.Add(msgDelay).Add(sp.Precision)

	if recvTime.Before(lhs) || recvTime.After(rhs) {
		return false
	}
	return true
}

// String returns a string representation of the Proposal.
//
// 1. height
// 2. round
// 3. block ID
// 4. POL round
// 5. first 6 bytes of signature
// 6. timestamp
//
// See BlockID#String.
func (p *Proposal) String() string {
	return fmt.Sprintf("Proposal{%v/%v (%v, %v) %X @ %s}",
		p.Height,
		p.Round,
		p.BlockID,
		p.POLRound,
		tmbytes.Fingerprint(p.Signature),
		CanonicalTime(p.Timestamp))
}

// ProposalSignBytes returns the proto-encoding of the canonicalized Proposal,
// for signing. Panics if the marshaling fails.
//
// The encoded Protobuf message is varint length-prefixed (using MarshalDelimited)
// for backwards-compatibility with the Amino encoding, due to e.g. hardware
// devices that rely on this encoding.
//
// See CanonicalizeProposal
func ProposalSignBytes(chainID string, p *tmproto.Proposal) []byte {
	pb := CanonicalizeProposal(chainID, p)
	bz, err := protoio.MarshalDelimited(&pb)
	if err != nil {
		panic(err)
	}

	return bz
}

// ToProto converts Proposal to protobuf
func (p *Proposal) ToProto() *tmproto.Proposal {
	if p == nil {
		return &tmproto.Proposal{}
	}
	pb := new(tmproto.Proposal)

	pb.BlockID = p.BlockID.ToProto()
	pb.Type = p.Type
	pb.Height = p.Height
	pb.Round = p.Round
	pb.PolRound = p.POLRound
	pb.Timestamp = p.Timestamp
	pb.Signature = p.Signature
	txKeys := make([]*tmproto.TxKey, 0, len(p.TxKeys))
	for _, txKey := range p.TxKeys {
		txKeys = append(txKeys, txKey.ToProto())
	}
	pb.TxKeys = txKeys
	pb.LastCommit = p.LastCommit.ToProto()
	eviD, err := p.Evidence.ToProto()
	if err != nil {
		panic(err)
	}
	pb.Evidence = eviD
	pb.Header = *p.Header.ToProto()
	pb.ProposerAddress = p.ProposerAddress

	return pb
}

// FromProto sets a protobuf Proposal to the given pointer.
// It returns an error if the proposal is invalid.
func ProposalFromProto(pp *tmproto.Proposal) (*Proposal, error) {
	if pp == nil {
		return nil, errors.New("nil proposal")
	}

	p := new(Proposal)

	blockID, err := BlockIDFromProto(&pp.BlockID)
	if err != nil {
		return nil, err
	}

	p.BlockID = *blockID
	p.Type = pp.Type
	p.Height = pp.Height
	p.Round = pp.Round
	p.POLRound = pp.PolRound
	p.Timestamp = pp.Timestamp
	p.Signature = pp.Signature
	txKeys, err := TxKeysListFromProto(pp.TxKeys)
	if err != nil {
		return nil, err
	}
	p.TxKeys = txKeys
	header, err := HeaderFromProto(&pp.Header)
	if err != nil {
		return nil, err
	}
	p.Header = header
	lastCommit, err := CommitFromProto(pp.LastCommit)
	p.LastCommit = lastCommit
	eviD := new(EvidenceList)
	eviD.FromProto(pp.Evidence)
	p.Evidence = *eviD
	p.ProposerAddress = pp.ProposerAddress

	return p, p.ValidateBasic()
}
