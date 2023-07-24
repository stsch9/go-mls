package mls

import (
	"fmt"
	"io"

	"golang.org/x/crypto/cryptobyte"
)

type groupContext struct {
	version                 protocolVersion
	cipherSuite             cipherSuite
	groupID                 GroupID
	epoch                   uint64
	treeHash                []byte
	confirmedTranscriptHash []byte
	extensions              []extension
}

func (ctx *groupContext) unmarshal(s *cryptobyte.String) error {
	*ctx = groupContext{}

	ok := s.ReadUint16((*uint16)(&ctx.version)) &&
		s.ReadUint16((*uint16)(&ctx.cipherSuite)) &&
		readOpaqueVec(s, (*[]byte)(&ctx.groupID)) &&
		s.ReadUint64(&ctx.epoch) &&
		readOpaqueVec(s, &ctx.treeHash) &&
		readOpaqueVec(s, &ctx.confirmedTranscriptHash)
	if !ok {
		return io.ErrUnexpectedEOF
	}

	if ctx.version != protocolVersionMLS10 {
		return fmt.Errorf("mls: invalid protocol version %d", ctx.version)
	}

	exts, err := unmarshalExtensionVec(s)
	if err != nil {
		return err
	}
	ctx.extensions = exts

	return nil
}

func (ctx *groupContext) marshal(b *cryptobyte.Builder) {
	b.AddUint16(uint16(ctx.version))
	b.AddUint16(uint16(ctx.cipherSuite))
	writeOpaqueVec(b, []byte(ctx.groupID))
	b.AddUint64(ctx.epoch)
	writeOpaqueVec(b, ctx.treeHash)
	writeOpaqueVec(b, ctx.confirmedTranscriptHash)
	marshalExtensionVec(b, ctx.extensions)
}

type pskType uint8

const (
	pskTypeExternal   pskType = 1
	pskTypeResumption pskType = 2
)

func (t *pskType) unmarshal(s *cryptobyte.String) error {
	if !s.ReadUint8((*uint8)(t)) {
		return io.ErrUnexpectedEOF
	}
	switch *t {
	case pskTypeExternal, pskTypeResumption:
		return nil
	default:
		return fmt.Errorf("mls: invalid PSK type %d", *t)
	}
}

func (t pskType) marshal(b *cryptobyte.Builder) {
	b.AddUint8(uint8(t))
}

type resumptionPSKUsage uint8

const (
	resumptionPSKUsageApplication resumptionPSKUsage = 1
	resumptionPSKUsageReinit      resumptionPSKUsage = 2
	resumptionPSKUsageBranch      resumptionPSKUsage = 3
)

func (usage *resumptionPSKUsage) unmarshal(s *cryptobyte.String) error {
	if !s.ReadUint8((*uint8)(usage)) {
		return io.ErrUnexpectedEOF
	}
	switch *usage {
	case resumptionPSKUsageApplication, resumptionPSKUsageReinit, resumptionPSKUsageBranch:
		return nil
	default:
		return fmt.Errorf("mls: invalid resumption PSK usage %d", *usage)
	}
}

func (usage resumptionPSKUsage) marshal(b *cryptobyte.Builder) {
	b.AddUint8(uint8(usage))
}

type preSharedKeyID struct {
	pskType pskType

	// for pskTypeExternal
	pskID []byte

	// for pskTypeResumption
	usage      resumptionPSKUsage
	pskGroupID GroupID
	pskEpoch   uint64

	pskNonce []byte
}

func (id *preSharedKeyID) unmarshal(s *cryptobyte.String) error {
	*id = preSharedKeyID{}

	if err := id.pskType.unmarshal(s); err != nil {
		return err
	}

	switch id.pskType {
	case pskTypeExternal:
		if !readOpaqueVec(s, &id.pskID) {
			return io.ErrUnexpectedEOF
		}
	case pskTypeResumption:
		if err := id.usage.unmarshal(s); err != nil {
			return err
		}
		if !readOpaqueVec(s, (*[]byte)(&id.pskGroupID)) || !s.ReadUint64(&id.pskEpoch) {
			return io.ErrUnexpectedEOF
		}
	default:
		panic("unreachable")
	}

	if !readOpaqueVec(s, &id.pskNonce) {
		return io.ErrUnexpectedEOF
	}

	return nil
}

func (id *preSharedKeyID) marshal(b *cryptobyte.Builder) {
	id.pskType.marshal(b)
	switch id.pskType {
	case pskTypeExternal:
		writeOpaqueVec(b, id.pskID)
	case pskTypeResumption:
		id.usage.marshal(b)
		writeOpaqueVec(b, []byte(id.pskGroupID))
		b.AddUint64(id.pskEpoch)
	default:
		panic("unreachable")
	}
	writeOpaqueVec(b, id.pskNonce)
}

func extractPSKSecret(cs cipherSuite, pskIDs []preSharedKeyID, psks [][]byte) ([]byte, error) {
	if len(pskIDs) != len(psks) {
		return nil, fmt.Errorf("mls: got %v PSK IDs and %v PSKs, want same number", len(pskIDs), len(psks))
	}

	_, kdf, _ := cs.hpke().Params()
	zero := make([]byte, kdf.ExtractSize())

	pskSecret := zero
	for i := range pskIDs {
		pskExtracted := kdf.Extract(psks[i], zero)

		pskLabel := pskLabel{
			id:    pskIDs[i],
			index: uint16(i),
			count: uint16(len(pskIDs)),
		}
		rawPSKLabel, err := marshal(&pskLabel)
		if err != nil {
			return nil, err
		}

		pskInput, err := cs.expandWithLabel(pskExtracted, []byte("derived psk"), rawPSKLabel, uint16(kdf.ExtractSize()))
		if err != nil {
			return nil, err
		}

		pskSecret = kdf.Extract(pskSecret, pskInput)
	}

	return pskSecret, nil
}

type pskLabel struct {
	id    preSharedKeyID
	index uint16
	count uint16
}

func (label *pskLabel) marshal(b *cryptobyte.Builder) {
	label.id.marshal(b)
	b.AddUint16(label.index)
	b.AddUint16(label.count)
}
