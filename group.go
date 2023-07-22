package mls

import (
	"fmt"
	"io"

	"golang.org/x/crypto/cryptobyte"
)

// http://www.iana.org/assignments/mls/mls.xhtml#mls-proposal-types
type proposalType uint16

const (
	proposalTypeAdd                    proposalType = 0x0001
	proposalTypeUpdate                 proposalType = 0x0002
	proposalTypeRemove                 proposalType = 0x0003
	proposalTypePSK                    proposalType = 0x0004
	proposalTypeReinit                 proposalType = 0x0005
	proposalTypeExternalInit           proposalType = 0x0006
	proposalTypeGroupContextExtensions proposalType = 0x0007
)

func (t *proposalType) unmarshal(s *cryptobyte.String) error {
	if !s.ReadUint16((*uint16)(t)) {
		return io.ErrUnexpectedEOF
	}
	switch *t {
	case proposalTypeAdd, proposalTypeUpdate, proposalTypeRemove, proposalTypePSK, proposalTypeReinit, proposalTypeExternalInit, proposalTypeGroupContextExtensions:
		return nil
	default:
		return fmt.Errorf("mls: invalid proposal type %d", *t)
	}
}

type proposal struct {
	proposalType           proposalType
	add                    *add                    // for proposalTypeAdd
	update                 *update                 // for proposalTypeUpdate
	remove                 *remove                 // for proposalTypeRemove
	preSharedKey           *preSharedKey           // for proposalTypePSK
	reInit                 *reInit                 // for proposalTypeReinit
	externalInit           *externalInit           // for proposalTypeExternalInit
	groupContextExtensions *groupContextExtensions // for proposalTypeGroupContextExtensions
}

func (prop *proposal) unmarshal(s *cryptobyte.String) error {
	*prop = proposal{}
	if err := prop.proposalType.unmarshal(s); err != nil {
		return err
	}
	switch prop.proposalType {
	case proposalTypeAdd:
		prop.add = new(add)
		return prop.add.unmarshal(s)
	case proposalTypeUpdate:
		prop.update = new(update)
		return prop.update.unmarshal(s)
	case proposalTypeRemove:
		prop.remove = new(remove)
		return prop.remove.unmarshal(s)
	case proposalTypePSK:
		prop.preSharedKey = new(preSharedKey)
		return prop.preSharedKey.unmarshal(s)
	case proposalTypeReinit:
		prop.reInit = new(reInit)
		return prop.reInit.unmarshal(s)
	case proposalTypeExternalInit:
		prop.externalInit = new(externalInit)
		return prop.externalInit.unmarshal(s)
	case proposalTypeGroupContextExtensions:
		prop.groupContextExtensions = new(groupContextExtensions)
		return prop.groupContextExtensions.unmarshal(s)
	default:
		panic("unreachable")
	}
}

type add struct {
	keyPackage keyPackage
}

func (a *add) unmarshal(s *cryptobyte.String) error {
	*a = add{}
	return a.keyPackage.unmarshal(s)
}

type update struct {
	leafNode leafNode
}

func (upd *update) unmarshal(s *cryptobyte.String) error {
	*upd = update{}
	return upd.leafNode.unmarshal(s)
}

type remove struct {
	removed uint32
}

func (rm *remove) unmarshal(s *cryptobyte.String) error {
	*rm = remove{}
	if !s.ReadUint32(&rm.removed) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

type preSharedKey struct {
	psk preSharedKeyID
}

func (psk *preSharedKey) unmarshal(s *cryptobyte.String) error {
	*psk = preSharedKey{}
	return psk.psk.unmarshal(s)
}

type reInit struct {
	groupID     GroupID
	version     protocolVersion
	cipherSuite cipherSuite
	extensions  []extension
}

func (ri *reInit) unmarshal(s *cryptobyte.String) error {
	*ri = reInit{}

	if !readOpaqueVec(s, (*[]byte)(&ri.groupID)) || !s.ReadUint16((*uint16)(&ri.version)) || !s.ReadUint16((*uint16)(&ri.cipherSuite)) {
		return io.ErrUnexpectedEOF
	}

	exts, err := unmarshalExtensionVec(s)
	if err != nil {
		return err
	}
	ri.extensions = exts

	return nil
}

type externalInit struct {
	kemOutput []byte
}

func (ei *externalInit) unmarshal(s *cryptobyte.String) error {
	*ei = externalInit{}
	if !readOpaqueVec(s, &ei.kemOutput) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

type groupContextExtensions struct {
	extensions []extension
}

func (exts *groupContextExtensions) unmarshal(s *cryptobyte.String) error {
	*exts = groupContextExtensions{}

	l, err := unmarshalExtensionVec(s)
	if err != nil {
		return err
	}
	exts.extensions = l

	return nil
}

type proposalOrRefType uint8

const (
	proposalOrRefTypeProposal  proposalOrRefType = 1
	proposalOrRefTypeReference proposalOrRefType = 2
)

func (t *proposalOrRefType) unmarshal(s *cryptobyte.String) error {
	if !s.ReadUint8((*uint8)(t)) {
		return io.ErrUnexpectedEOF
	}
	switch *t {
	case proposalOrRefTypeProposal, proposalOrRefTypeReference:
		return nil
	default:
		return fmt.Errorf("mls: invalid proposal or ref type %d", *t)
	}
}

type proposalRef []byte

type proposalOrRef struct {
	typ       proposalOrRefType
	proposal  *proposal   // for proposalOrRefTypeProposal
	reference proposalRef // for proposalOrRefTypeReference
}

func (propOrRef *proposalOrRef) unmarshal(s *cryptobyte.String) error {
	*propOrRef = proposalOrRef{}

	if err := propOrRef.typ.unmarshal(s); err != nil {
		return err
	}

	switch propOrRef.typ {
	case proposalOrRefTypeProposal:
		propOrRef.proposal = new(proposal)
		return propOrRef.proposal.unmarshal(s)
	case proposalOrRefTypeReference:
		if !readOpaqueVec(s, (*[]byte)(&propOrRef.reference)) {
			return io.ErrUnexpectedEOF
		}
		return nil
	default:
		panic("unreachable")
	}
}

type commit struct {
	proposals []proposalOrRef
	path      *updatePath // optional
}

func (c *commit) unmarshal(s *cryptobyte.String) error {
	*c = commit{}

	err := readVector(s, func(s *cryptobyte.String) error {
		var propOrRef proposalOrRef
		if err := propOrRef.unmarshal(s); err != nil {
			return err
		}
		c.proposals = append(c.proposals, propOrRef)
		return nil
	})
	if err != nil {
		return err
	}

	var hasPath bool
	if !readOptional(s, &hasPath) {
		return io.ErrUnexpectedEOF
	} else if hasPath {
		c.path = new(updatePath)
		if err := c.path.unmarshal(s); err != nil {
			return err
		}
	}

	return nil
}

type groupInfo struct {
	groupContext    groupContext
	extensions      []extension
	confirmationTag []byte
	signer          uint32
	signature       []byte
}

func (info *groupInfo) unmarshal(s *cryptobyte.String) error {
	*info = groupInfo{}

	if err := info.groupContext.unmarshal(s); err != nil {
		return err
	}

	exts, err := unmarshalExtensionVec(s)
	if err != nil {
		return err
	}
	info.extensions = exts

	if !readOpaqueVec(s, &info.confirmationTag) || !s.ReadUint32(&info.signer) || !readOpaqueVec(s, &info.signature) {
		return err
	}

	return nil
}

func (info *groupInfo) marshalTBS(b *cryptobyte.Builder) {
	info.groupContext.marshal(b)
	marshalExtensionVec(b, info.extensions)
	writeOpaqueVec(b, info.confirmationTag)
	b.AddUint32(info.signer)
}

func (info *groupInfo) marshal(b *cryptobyte.Builder) {
	info.marshalTBS(b)
	writeOpaqueVec(b, info.signature)
}

type groupSecrets struct {
	joinerSecret []byte
	pathSecret   []byte // optional
	psks         []preSharedKeyID
}

func (sec *groupSecrets) unmarshal(s *cryptobyte.String) error {
	*sec = groupSecrets{}

	if !readOpaqueVec(s, &sec.joinerSecret) {
		return io.ErrUnexpectedEOF
	}

	var hasPathSecret bool
	if !readOptional(s, &hasPathSecret) {
		return io.ErrUnexpectedEOF
	} else if hasPathSecret && !readOpaqueVec(s, &sec.pathSecret) {
		return io.ErrUnexpectedEOF
	}

	return readVector(s, func(s *cryptobyte.String) error {
		var psk preSharedKeyID
		if err := psk.unmarshal(s); err != nil {
			return err
		}
		sec.psks = append(sec.psks, psk)
		return nil
	})
}

type welcome struct {
	cipherSuite        cipherSuite
	secrets            []encryptedGroupSecrets
	encryptedGroupInfo []byte
}

func (w *welcome) unmarshal(s *cryptobyte.String) error {
	*w = welcome{}

	if !s.ReadUint16((*uint16)(&w.cipherSuite)) {
		return io.ErrUnexpectedEOF
	}

	err := readVector(s, func(s *cryptobyte.String) error {
		var sec encryptedGroupSecrets
		if err := sec.unmarshal(s); err != nil {
			return err
		}
		w.secrets = append(w.secrets, sec)
		return nil
	})
	if err != nil {
		return err
	}

	if !readOpaqueVec(s, &w.encryptedGroupInfo) {
		return io.ErrUnexpectedEOF
	}

	return nil
}

func (w *welcome) findSecret(ref keyPackageRef) *encryptedGroupSecrets {
	for i, sec := range w.secrets {
		if sec.newMember.Equal(ref) {
			return &w.secrets[i]
		}
	}
	return nil
}

func (w *welcome) process(ref keyPackageRef, initKeyPriv, signerPub []byte) error {
	cs := w.cipherSuite

	sec := w.findSecret(ref)
	if sec == nil {
		return fmt.Errorf("mls: encrypted group secrets not found for provided key package ref")
	}

	rawGroupSecrets, err := cs.decryptWithLabel(initKeyPriv, []byte("Welcome"), w.encryptedGroupInfo, sec.encryptedGroupSecrets.kemOutput, sec.encryptedGroupSecrets.ciphertext)
	if err != nil {
		return err
	}
	var groupSecrets groupSecrets
	if err := unmarshal(rawGroupSecrets, &groupSecrets); err != nil {
		return err
	}

	if len(groupSecrets.psks) > 0 {
		return fmt.Errorf("TODO: welcome.process with psks")
	}

	_, kdf, aead := cs.hpke().Params()
	kdfSecret := make([]byte, kdf.ExtractSize())
	kdfSalt := groupSecrets.joinerSecret
	extractedJoinerSecret := kdf.Extract(kdfSecret, kdfSalt)

	welcomeSecret, err := cs.deriveSecret(extractedJoinerSecret, []byte("welcome"))
	if err != nil {
		return err
	}

	welcomeNonce, err := cs.expandWithLabel(welcomeSecret, []byte("nonce"), nil, uint16(aead.NonceSize()))
	if err != nil {
		return err
	}
	welcomeKey, err := cs.expandWithLabel(welcomeSecret, []byte("key"), nil, uint16(aead.KeySize()))
	if err != nil {
		return err
	}

	welcomeCipher, err := aead.New(welcomeKey)
	if err != nil {
		return err
	}
	rawGroupInfo, err := welcomeCipher.Open(nil, welcomeNonce, w.encryptedGroupInfo, nil)
	if err != nil {
		return err
	}

	var groupInfo groupInfo
	if err := unmarshal(rawGroupInfo, &groupInfo); err != nil {
		return err
	}

	groupInfoTBS, err := marshalTBS(&groupInfo)
	if err != nil {
		return err
	}
	if !cs.verifyWithLabel(signerPub, []byte("GroupInfoTBS"), groupInfoTBS, groupInfo.signature) {
		return fmt.Errorf("mls: group info signature verification failed")
	}

	// TODO: verify confirmation tag in group info

	return nil
}

type encryptedGroupSecrets struct {
	newMember             keyPackageRef
	encryptedGroupSecrets hpkeCiphertext
}

func (sec *encryptedGroupSecrets) unmarshal(s *cryptobyte.String) error {
	*sec = encryptedGroupSecrets{}
	if !readOpaqueVec(s, (*[]byte)(&sec.newMember)) {
		return io.ErrUnexpectedEOF
	}
	if err := sec.encryptedGroupSecrets.unmarshal(s); err != nil {
		return err
	}
	return nil
}
