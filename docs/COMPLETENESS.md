# Completeness

This is the list of everything a full mail client does (Thunderbird, Apple Mail, Gmail on the web), and the automated
check that proves each row. Release 1.0.0 is not ready until every row that is not dropped passes.

`tools/completeness.sh` runs every check and then `tools/completeness.py` reads this file and fails if a row has no
passing check. A row has one of three states:

* **shipped**: built, and each check named in the row passes.
* **hardware**: built and checked as far as software can check it; what only a person at a screen or a printer can see
  is written in the note and is the one thing left out.
* **dropped**: deliberately not built, with the reason.

Checks are named by kind: `go:` is a Go test with no server needed, `lab:` is a Go test against the Podman lab server
(`lab/stalwart.sh`: IMAP, POP3, JMAP, ManageSieve, CalDAV and CardDAV), `ui:` is a line of the browser test
`ui/test/features.mjs`, `live:` is a line of `ui/test/live_add.mjs` (real sign-in on Microsoft, the lab for the rest),
`smoke:` is `ui/test/smoke.mjs`, `i18n:` is `tools/i18n-check.mjs`, and `build:` is a build the runner does itself.

| ID | Feature | State | Checks | Note |
|---|---|---|---|---|
| A1 | Add an account by typing the email address, provider found by itself (known providers, MX, Mozilla ISPDB, autoconfig, SRV) | shipped | go:TestKnownMXAndAutoconfig; go:TestGmailOnmicrosoftIspdbAndPop3; smoke:a Gmail address shows the app password steps | manual fields only as the fallback |
| A2 | Microsoft (Outlook.com, Hotmail, work and school) one click sign in | shipped | live:Microsoft account added with one click and a real sign-in; live:Microsoft shows one button and no client fields | the built in Entra app, nothing to configure |
| A3 | Gmail and Google Workspace: app password steps on screen, working out of the box; one click Google sign in when a Google client is built in | shipped | live:password login with a spaced app password works against the lab; live:Gmail IMAP server was filled in; go:TestSavedSignInAppIsUsed | the login is proved against the lab with a password; a real Google app password was not made (it needs the account holder), the Google server path is proved with a token in `TestGmailOverIMAPLive` |
| A4 | IMAP and SMTP accounts (self hosted, any provider) | shipped | lab:TestIMAPLab; live:IMAP lab account added through the add-account screen | |
| A5 | POP3 accounts, leave on server, no duplicates, send by SMTP | shipped | lab:TestFeatPOP3Lab; live:POP3 lab account added; live:the mail was downloaded over POP3 | |
| A6 | JMAP accounts | shipped | lab:TestJMAPLab | |
| A7 | Several accounts at once, one combined inbox | shipped | smoke:ui smoke ok | the demo build has three accounts |
| A8 | Account name and signature, remove an account | shipped | go:TestRemoveAndUpdateAccount | |
| A9 | Servers with their own or self signed certificate | shipped | live:IMAP lab account added through the add-account screen | the lab server is self signed |
| B1 | Folders, labels and custom folders, with unread counts | shipped | lab:TestFeatFoldersAndBulkLab | |
| B2 | Conversation view, and the option to list every message on its own | shipped | go:TestIMAPStyleThreading; go:TestFlatListShowsEveryMessage | |
| B3 | Messages shown safely: scripts never run, remote images held back until allowed | shipped | smoke:ui smoke ok; ui:remote images are blocked until allowed; ui:Load images lets them in | |
| B4 | Attachments: list, download, inline images | shipped | lab:TestIMAPLab; go:TestInlineImageKeepsItsContentID | |
| B5 | Search with operators (from, to, cc, bcc, subject, filename, label, in, is, has, before, after, older_than, newer_than, larger, smaller, phrases, exclude, OR) | shipped | go:TestSearchOperators; lab:TestFeatSearchOperatorsLab; ui:a search with an operator and OR finds the message | |
| B6 | Saved searches | shipped | ui:the saved search is in the sidebar; ui:the saved search runs when clicked | |
| B7 | Star, read and unread, archive, trash, spam, move and label, reaching the server | shipped | lab:TestIMAPLab; lab:TestJMAPLab | |
| B8 | Snooze | shipped | go:TestThreadsSearchArchiveSnooze | |
| B9 | Print | hardware | ui:print and source buttons are on the message | the print document is built and handed to the system print dialog; paper output needs a printer |
| B10 | View the message source | shipped | ui:view source shows the message | |
| B11 | Save a message as .eml, conversations as mbox | shipped | lab:TestFeatExportImportLab; go:TestImportMboxExportAndAttachment | |
| B12 | Unsubscribe from mailing lists (one click, mailto, web page) | shipped | lab:TestFeatUnsubscribeAndReceiptLab; ui:a mailing list message offers Unsubscribe | |
| B13 | Calendar invitations: show the event and answer yes, maybe or no; add to a calendar | shipped | lab:TestFeatInviteRSVPLab; go:TestParseInvite; go:TestReplyAndRoundTrip; ui:the invitation shows its event and the three answers | |
| B14 | Read receipts: ask for one, answer one, set a rule for it | shipped | lab:TestFeatUnsubscribeAndReceiptLab; ui:a read receipt request is asked about; ui:declining the receipt hides the question; ui:the options hold read receipt, sign and encrypt | |
| B15 | Signed and encrypted mail: check the signature, decrypt, keep the signer's key | shipped | lab:TestFeatSecurityLab; go:TestPGPSignVerifyAndTamper; go:TestPGPEncryptDecrypt; go:TestSMIMESignVerifyEncrypt | OpenPGP (RFC 3156) and S/MIME (RFC 8551) |
| C1 | Write, reply, reply all and forward, threads kept | shipped | lab:TestIMAPLab; lab:TestJMAPLab | |
| C2 | Rich text editor | shipped | ui:the bold button formats the text | |
| C3 | Attachments by button, drop and paste; a warning over 25 MB | shipped | smoke:ui smoke ok; ui:a message over 25 MB is questioned and not sent | |
| C4 | Suggestions for recipients from contacts | shipped | ui:typing a name suggests the contact; go:TestContactsSuggest | |
| C5 | Drafts saved as you write, and kept on the server | shipped | smoke:ui smoke ok; go:TestOutboxUndoAndOps | |
| C6 | Undo send | shipped | lab:TestIMAPLab; lab:TestFeatScheduledSendAndUndoLab | |
| C7 | Schedule a message for later; cancel it before then | shipped | lab:TestFeatScheduledSendAndUndoLab; ui:send later puts the message in the outbox for a later time; ui:the scheduled message can be cancelled | |
| C8 | Templates (canned replies) | shipped | ui:the template is listed; ui:a template fills the composer | |
| C9 | Several identities, aliases and signatures | shipped | go:TestIdentitySignatureAndReplyTo; ui:the alias is added as a second identity; ui:the From list has the account | aliases from Gmail, Microsoft and JMAP are fetched; others are added by hand |
| C10 | Spell check while writing | shipped | ui:spelling is checked while writing | the system spelling dictionary does the checking |
| C11 | Sign and encrypt outgoing mail | shipped | lab:TestFeatSecurityLab; ui:an OpenPGP key is made | a recipient with no key is named and nothing is sent |
| C12 | Failed sends retried by themselves when the reason is temporary | shipped | go:TestTemporaryFailuresAreRetried | |
| D1 | Filters and rules on new mail (conditions, actions, order, run now) | shipped | go:TestMatch; go:TestRulesRunOnNewMail; lab:TestFeatRulesLab; ui:the filter is listed | |
| D2 | Server side filters (Sieve over ManageSieve): list, edit, check, save, run, delete | shipped | lab:TestFeatManageSieveLab; go:TestVacationRoundTrip | |
| D3 | Vacation reply kept on the server | shipped | lab:TestFeatVacationLab; ui:the vacation screen opens | Gmail, Microsoft, JMAP; IMAP through the Sieve script |
| D4 | Create, rename and delete folders and labels | shipped | lab:TestFeatFoldersAndBulkLab | |
| D5 | Mark everything read, empty trash and spam | shipped | lab:TestFeatFoldersAndBulkLab | |
| E1 | Address book: add, edit, delete, write to | shipped | ui:the contact is listed; go:TestContactsSuggest | |
| E2 | CardDAV address books | shipped | lab:TestFeatCalDAVCardDAVLab; go:TestCalDAVAndCardDAV; go:TestVCardRoundTrip | |
| E3 | CalDAV calendars: see, add, change and delete events | shipped | lab:TestFeatCalDAVCardDAVLab; ui:the calendar and contacts server screen answers (it needs a matching account) | |
| F1 | Import an mbox file | shipped | go:TestMboxSplitsAndUnquotes; go:TestImportMboxExportAndAttachment; go:TestCommandLine | |
| F2 | Import a Thunderbird profile | shipped | go:TestThunderbirdProfile | |
| F3 | Import screen | shipped | ui:the import mail screen opens | |
| G1 | A command for every action, with keyboard shortcuts you can change | shipped | ui:the new key opens the composer; ui:the old key no longer opens the composer; ui:the new key is saved in settings | |
| G2 | Command palette | shipped | ui:the palette finds a folder; ui:a command runs from the palette | |
| G3 | Light and dark theme, density | shipped | smoke:ui smoke ok | |
| G4 | Desktop notifications for new mail | hardware | go:TestNewMailEvent | the event, the Windows toast call and the Linux call are checked in software; the picture on a real screen is not |
| G5 | Icon in the notification area, menu, unread count, keep running when the window closes | hardware | go:TestMenuAndTooltip; build:windows tray | the menu and the build for both systems are checked; the icon on a real desktop is not |
| G6 | English and thirteen languages, one glossary | shipped | go:TestEveryLanguageHasEveryPhrase; go:TestLanguageListMatchesCatalogs; i18n:ok; ui:German: the window and the page language change; ui:Arabic: the window is right to left; ui:English comes back | window text only: the messages a mail server sends back stay in the server's language |
| G7 | Works on a small screen | shipped | ui:the page fits a phone width without sideways scrolling | |
| G8 | Usable with a screen reader | shipped | ui:every icon button has a name for screen readers | |
| G9 | Command line (`halyard` and `halyardctl`): send, search, list, read and the rest | shipped | go:TestCommandLine; go:TestPackagesShipTheCommandLine | |
| G10 | MCP server, read and write, on by default | shipped | lab:TestMCPIMAPLab; lab:TestMCPJMAPLab | |
| G11 | Works offline, changes wait and are replayed | shipped | go:TestOfflineQueueReplays | |
| G12 | Handles mailto: links as the default mail program | shipped | go:TestPackagesRegisterTheMailtoHandler | |
| G13 | Installs on Arch, EmberRTOS Desktop and Windows | shipped | build:packages | proved by the package builds in the release run |
| H1 | RSS and newsgroup (NNTP) reading | dropped | - | not mail: RSS is dropped by decision, and newsgroups are a different protocol with no use in what this client is for |
| H2 | Check for updates: Windows checks the signed release on patrickryan01/halyard and verifies its SHA-256 before offering the installer; Arch and EmberRTOS Desktop say updates come from the package manager | shipped | go:TestUpdateCheckVerifiesHash; go:TestUpdateOnPackageManagerBuilds | the release job publishes the installer and its .sha256 |
| H3 | Contact groups: create, edit, send to a group, kept on the CardDAV server as group cards where it supports them | shipped | go:TestSendingToAGroupSendsToItsMembers; go:TestGroupVCardRoundTrip; lab:TestFeatCalDAVCardDAVLab | vCard 4.0 KIND:group with MEMBER (RFC 6350) |
