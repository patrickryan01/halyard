// Package mailtypes holds the types every layer shares: accounts, folders,
// messages and the actions a person can take on them.
package mailtypes

import "time"

// Account kinds. Each kind is one provider plugin.
const (
	KindGoogle    = "google"
	KindMicrosoft = "microsoft"
	KindIMAP      = "imap"
	KindJMAP      = "jmap"
)

// Folder roles. A role is what a folder means, whatever the provider calls it.
const (
	RoleInbox   = "inbox"
	RoleSent    = "sent"
	RoleDrafts  = "drafts"
	RoleTrash   = "trash"
	RoleSpam    = "spam"
	RoleArchive = "archive"
	RoleStarred = "starred"
	RoleAll     = "all"
	RoleCustom  = "custom"
)

type Account struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Email string `json:"email"`
	Name  string `json:"name"`
	// Settings is provider configuration (hosts, ports, auth mode). It never
	// holds secrets; those live in the secret store keyed by the account ID.
	Settings  map[string]string `json:"settings"`
	Signature string            `json:"signature"`
	Color     string            `json:"color"`
	SyncState string            `json:"-"`
	LastSync  time.Time         `json:"lastSync"`
	LastError string            `json:"lastError"`
}

type Folder struct {
	ID        string `json:"id"`
	AccountID string `json:"accountId"`
	RemoteID  string `json:"remoteId"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	// IsLabel is true for Gmail style labels (a message can carry several) and
	// false for folders (a message lives in one).
	IsLabel bool `json:"isLabel"`
	Unread  int  `json:"unread"`
	Total   int  `json:"total"`
}

type Address struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Attachment struct {
	ID          string `json:"id"` // provider part or attachment ID
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	Inline      bool   `json:"inline"`
	ContentID   string `json:"contentId,omitempty"`
}

type Message struct {
	ID         string       `json:"id"` // local ID: accountID + "/" + RemoteID
	AccountID  string       `json:"accountId"`
	RemoteID   string       `json:"remoteId"`
	ThreadID   string       `json:"threadId"` // local thread key, accountID + "/" + provider thread
	Subject    string       `json:"subject"`
	From       Address      `json:"from"`
	To         []Address    `json:"to"`
	Cc         []Address    `json:"cc"`
	Bcc        []Address    `json:"bcc"`
	ReplyTo    []Address    `json:"replyTo,omitempty"`
	Date       time.Time    `json:"date"`
	Snippet    string       `json:"snippet"`
	BodyText   string       `json:"bodyText"`
	BodyHTML   string       `json:"bodyHtml"`
	MessageID  string       `json:"messageId"`
	InReplyTo  string       `json:"inReplyTo"`
	References []string     `json:"references"`
	Folders    []string     `json:"folders"` // remote folder IDs the message is in
	Read       bool         `json:"read"`
	Starred    bool         `json:"starred"`
	Draft      bool         `json:"draft"`
	Attach     []Attachment `json:"attachments"`
	Snoozed    *time.Time   `json:"snoozedUntil,omitempty"`
}

// Thread is the list row: the newest message plus what the conversation holds.
type Thread struct {
	ID           string     `json:"id"`
	AccountID    string     `json:"accountId"`
	Subject      string     `json:"subject"`
	Snippet      string     `json:"snippet"`
	Participants []string   `json:"participants"`
	Date         time.Time  `json:"date"`
	Count        int        `json:"count"`
	Unread       int        `json:"unread"`
	Starred      bool       `json:"starred"`
	HasAttach    bool       `json:"hasAttachments"`
	Folders      []string   `json:"folders"`
	Snoozed      *time.Time `json:"snoozedUntil,omitempty"`
}

type Contact struct {
	ID    string `json:"id,omitempty"` // provider ID; empty for contacts learned from mail
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone,omitempty"`
	Org   string `json:"org,omitempty"`
}

// Outgoing is a message to send. Attachments carry their bytes.
type Outgoing struct {
	AccountID   string
	From        Address
	To, Cc, Bcc []Address
	Subject     string
	BodyText    string
	BodyHTML    string
	InReplyTo   string
	References  []string
	ThreadHint  string // provider thread ID when replying, so Gmail keeps the thread
	Attachments []OutAttachment
}

type OutAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Action is something done to messages. Providers translate it to their own
// calls: Gmail label changes, Graph moves and patches, IMAP flags and COPY.
type Action struct {
	Kind     string   `json:"kind"` // archive, trash, untrash, spam, read, unread, star, unstar, move, label, unlabel, delete
	FolderID string   `json:"folderId,omitempty"`
	IDs      []string `json:"ids"` // remote message IDs
}

const (
	ActArchive = "archive"
	ActTrash   = "trash"
	ActUntrash = "untrash"
	ActSpam    = "spam"
	ActRead    = "read"
	ActUnread  = "unread"
	ActStar    = "star"
	ActUnstar  = "unstar"
	ActMove    = "move"
	ActLabel   = "label"
	ActUnlabel = "unlabel"
	ActDelete  = "delete"
)

// Change is what one sync pass reports.
type Change struct {
	Upsert  []Message
	Delete  []string // remote message IDs
	Folders []Folder // full folder list when it was read
	// Flags updates read and starred state, and which folders a message is in,
	// for messages whose body did not change.
	Flags []FlagUpdate
}

// FlagUpdate changes state of a message the cache already holds.
type FlagUpdate struct {
	RemoteID string
	Read     bool
	Starred  bool
	// Folders is the full set of remote folder IDs when the provider reports
	// it. Nil means the update does not touch folders.
	Folders []string
}
