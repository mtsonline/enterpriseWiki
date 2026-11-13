# User guide

## Structure

- **Categories** organise the wiki like a table of contents. They can have any number of sub-categories and carry an outline number (e.g. `1.2.3`).
- **Elements** are in categories. The standard type is the **page** (formatted text or Markdown). Further types come through plugins, e.g. the notice box.
- Every category can have a **default page**. It is shown directly when the category is opened.

On the left you see the **page tree**. With the arrow buttons you expand and collapse sub-categories. At the top you find search, language selection, the **Edit** button and your user menu.

## Reading and language

Choose the **language** at the top. Interface and content change together. If a page does not exist in your language yet, you see the original language with a notice. Your choice is stored in the browser. Under *My profile* you can set a preferred language.

## Edit mode

As long as edit mode is off, every page looks as it does for all readers. With **Edit** (only visible if you have edit permissions) you switch it on. Then the actions appear:

**In a category:** *New page*, *New sub-category*, *Edit category* (title, description and tags in all languages), *Category settings* (default page, address, permissions, history), *Move*, *Duplicate*, *Delete* and arrows for sorting the sub-categories. The **default page** of the category (the text shown directly in the category) has its own buttons *Edit page*, *Tags* and *History*; in the list *Pages*, *Edit* leads directly into the editor.

**On a page:** *Edit*, *History*, *Tags*, *Set as default page*, *Move*, *Duplicate*, *Delete* and the overview **Translations**.

### Drafts, preview, templates and publishing
In the editor, **Save as draft** stores the state without publishing it. Only you and the editors of the category see drafts; they are listed in the category under *Drafts*. **Preview** shows the page as readers will see it. **Save as template** stores the content as a template (for you, the category or – with the permission to do so – for everyone); with *New page* you choose a template.

**Publish** applies the change immediately. **Publish with options** asks for the target category (only categories in which you may create), a point in time (scheduled publishing) and whether watchers are notified. Plugins can add further options, e.g. "as news" in a carousel.

### New sub-category
The currently open category is automatically selected as the parent category. You can enter title and description for every language.

### Editing pages
Pages are stored as **Markdown** (GitHub Markdown). There are two editors; you choose which one you use in your **profile** (*Editor for pages*), otherwise the default of the site applies.

**Visual editor** (default):
- Toolbar: paragraph format (paragraph, heading 1–4, code block), bold, italic, strikethrough, code, link, bulleted list, numbered list, **task list**, quote, table, image, horizontal rule, macro, undo/redo. In a table, additional buttons for rows and columns appear.
- Markdown while typing: `# ` to `###### ` → heading, `- ` → list, `1. ` → numbered list, `> ` → quote, ` ``` ` → code block, `---` → horizontal rule, `[ ] ` at the start of a list item → task, `**bold**`, `*italic*`, `~~strikethrough~~`, `` `code` ``. Pasted Markdown text is converted.
- Keyboard: Ctrl+B/I/E/K (bold, italic, code, link), Ctrl+Shift+X (strikethrough), Ctrl+Shift+7/8/9 (lists), Ctrl+Alt+0…4 (paragraph, headings), Ctrl+Z/Ctrl+Y. **Alt+F10** jumps into the toolbar (arrow keys choose, Escape back into the text). **Tab** indents in lists and jumps to the next cell in tables; **Escape, then Tab** always leaves the editor. **Enter** on a selected macro or image opens its settings.
- **Markdown** (on the right of the toolbar) shows the source; a second click switches back.
- **Live editing:** if several people edit the same page, everyone sees the changes and cursors of the others immediately. Above the text it says who is co-editing; coming and going is announced for screen readers. Undo only affects your own changes. If someone saves, the others get a notice and continue saving without a conflict. Every person has their own combination of colour and shape (●, ■, ▲, ◆); the same marker is at the cursor and in the list of co-editors above the text. Live editing happens in the currently active view – visual or **Markdown source** (also there with the cursors of the others). Whoever switches with *Markdown* switches for everyone; this is announced, and the focus stays where it is for every person.

**Simple editor:** a lean input field (formatted or Markdown) without live editing, e.g. for older devices.

- **Tags** are directly below the content (separated by commas); they apply to all languages of the page.
- Optionally enter a **change note**. It appears in the history.
- If someone saved the page at the same time (without live editing), you are warned instead of overwriting the other change. Then reload and apply your changes again.

### Files
With *New page*, choose the element type **File**, upload the file and describe it. PDFs, images and videos are shown directly, other files are available for download. Uploading and saving a new file creates a new version. Earlier files stay in the history. The text of PDFs can be found through the search.

### Media library
Under *User menu → Media* (`/media`) are all images, documents and other files of the site. Every file belongs to a category and has its permissions. **Invisible categories** (*Category settings → Invisible category*) are suitable as "folders" for media: they do not appear in navigation, menus, lists and search, but have completely normal permissions.

When **uploading** you set:
- **Category** – determines the permissions. Whoever may see a page in which the file is embedded also sees it there.
- **Download allowed** – switched off, the file can only be viewed: no download button, no direct link, the context menu is blocked. PDFs are then shown page by page as images. (Screenshots can technically never be prevented – that is what watermarks are for.)
- **Watermark** – none; *do not store the original* (only the version with your name); *show with your watermark, original for download*; *burn in the name of the viewer when viewing*; *when viewing and downloading*. Name and date are preselected, both can be deselected, plus an optional comment. Applies to images and PDFs.
- **Own permissions** – replace the permissions of the category and of the pages that show the file (view, download, edit, manage for people, groups, roles, signed-in users or guests). You can only grant permissions you have yourself.

In the **detail view** you see size, versions and on which pages the file is used; with edit permission you change texts, upload a new version or open the **image editor** (crop, rotate, flip, scale, brightness/contrast/saturation, greyscale, blur). Every edit is a new version; all pages automatically show the newest, earlier versions can be restored.

**Office files** (texts, spreadsheets, presentations) open directly in the browser with *Edit in …* or *View in …* if the
administration has set up an office program (Collabora, OnlyOffice or Microsoft 365). With edit permission every save
becomes a new version; without download permission you see the file read-only, without downloading, printing and
copying. If "always open directly" is set, a click on the file starts the program immediately.
*Close* leads back into the wiki.

### Images in pages
In the editor, the button **Insert image** opens the media library: choose an image or upload a new one (default: category of the page) and enter an alternative text that describes the image for people who cannot see it. Other files are inserted as a link. The macro **Image gallery** shows selected images (`media="12,13,14"`) or all images of a category as a grid; a click opens the image large, with the arrow keys you browse on.

### Macros
Macros are building blocks that are filled in when the page is shown. In the visual editor the button **Insert macro** (⚙) inserts one; macros with content (e.g. notice box) wrap selected paragraphs. An inserted macro appears as a dashed box with name, settings and the buttons *Edit macro* (✎) and *Remove macro* (×); macros in the text (e.g. status label, text colour) as a small block. Some macros have their own button in the toolbar (◉ status label, **A** text colour and font); the selected text is taken over.

In the Markdown source, macros are directives: `::toc{levels=3}` (without content), `:::panel{type=info title="Attention"}` … `:::` (with content; nested macros with more colons on the outside) and `:status[DONE]{color=green}` (in the text).

| Macro | Effect |
|---|---|
| Table of contents | lists the headings of the page with anchors |
| Sub-pages | sub-categories and pages of this (or another) category |
| Recently changed | the most recently changed pages |
| Notice box | info, note, tip, warning or success, with title |
| Expand section | content that only appears after a click |
| Status label | coloured label such as DONE |
| Text colour and font | text colour, highlighting, font and size from the palette of the site (all combinations easily readable) |
| Columns / cards | content side by side; separate columns or cards with a horizontal rule |
| Section, hero banner, button link, spacing | building blocks for website pages (especially with the template *CMS*) |
| Shell command | output of a command on the server; only runs once a super administrator has approved exactly this command |
| HTTP request | result of a web address (text, HTML, JSON as a table); only approved addresses |
| News from several sources | the newest elements of any number of sources and entries of external RSS/Atom feeds together, as cards, list or headlines |
| Blog posts from several sources | posts of several sources in the look of the blog (list, cards, compact) |
| FAQ from several sources | questions of several sources as one FAQ, one expandable section per category |
| Recently changed, upcoming events | also with freely selectable sources |

You put together **sources** in macros, blog, FAQ, calendar, carousel and property reports in the same way with the **source builder**: on the left are the building blocks, a click (or Enter) adds one under **Elements from**, and with the mouse they can also be dragged into an area. **Elements from** – one source is enough; **Exclude** – elements matched by one of them never appear (e.g. tag "internal"); **Must also match** – only elements additionally matched by each of them. Every entry can be moved into another area through its selection field (or by dragging the handle ⠿). Below, the preview shows how many elements result with your permissions. Building blocks are a **category** (with or without sub-categories), a **tag**, an **element type**, **single elements**, a **search** (all hits for search terms), a **property filter** (e.g. `status = open; due <= today+7d` – all elements whose properties match), depending on the place also an **external feed** (RSS/Atom), **another calendar** or an **iCal subscription**. In the Markdown source, sources are separated by semicolons, e.g. `::news{source="category:12+sub; tag:hr; feed:https://example.org/rss"}`; the filters of a property source are separated by `&&` there (`props:status = open && due <= today+7d`). Exclude is written as `-`, "must also match" as `+` before the source (`category:12+sub; -tag:internal; +props:status = open`). Without a source – or only with exclusions – the current category with sub-categories applies.

**Maximum and pages:** for lists from sources (news, blog, FAQ) you choose the **maximum number of entries** and whether the list is **split into pages** – with how many **entries per page**. Pages change without reloading, the focus jumps to the start of the list; every page has its own link. The administration sets the upper limit (default 1000).

In macros everyone only sees what they may see anyway.

### Processes (workflows)
With *New page*, choose the element type **Workflow**. With *Template* you take over a finished process (e.g. *HR: application*) and adapt it:

- **Start form:** fields that are filled in at the start (text, e-mail, date, choice, file, confirmation …).
- **Steps:** every step has a kind:

| Kind | Effect |
|---|---|
| Task | those responsible (starting person, user, group or role) decide with one of the **actions** (e.g. *Invite to interview*, *Reject*); every action leads to a next step and can require a note |
| Approval | four-eyes principle: a certain number of approvals or all members of a group/role must approve; a rejection (with reason) ends the approval |
| Condition | checks data from the form automatically (e.g. "amount greater than 1000", "position is software development") and branches |
| Send e-mail | sends a prepared e-mail automatically (recipients, Cc, sender, reply address, subject, text) |
| Webhook | hands the case over to another system (e.g. HR software) |
| Waiting time | waits days/hours or until a date from the form (e.g. the start date) |
| Message | sends a message in the wiki (bell) and/or by e-mail to `starter`, `actor`, `assignees`, `user:NAME`, `group:NAME`, `role:NAME` or addresses |
| Set values | sets values of the case: text with placeholders or a formula (`round(quantity * price, 2)`, `if(amount > 1000, 1, 0)` …); later steps use them as `{{field.NAME}}` and in conditions |
| HTTP request | queries another system (only addresses of the allow list `plugins.macros.http_allow`) and takes over values of the JSON answer (path such as `data.status` or `entries.0.id`) |
| Create page | creates a page (Markdown with placeholders) in a category and remembers ID and address for later steps |
| Set properties | sets properties of a page (e.g. of the one just created: `{{field.page}}`) |
| Permissions | grants a person (user name, alias, e-mail or ID, usually a placeholder such as `{{trigger.data.username}}`) or group read, edit or manage permissions on a category – if desired only for **N days**, after which they are withdrawn automatically – or withdraws them |
| Group membership | adds a person to a group or removes them from it |
| Move page | moves a page into another category, e.g. into the archive |
| Create file | writes a document from a template into the media library: PDF, Word (.docx), Excel (.xlsx), CSV, JSON, text or Markdown – lines with `#`, `##`, `###` become headings, if desired with all fields of the case (Excel and CSV always as a table). ID and address of the file end up in fields (`{{field.file}}`) |
| Calendar entry | creates an event in a calendar: title, start (date, date with time, placeholder such as `{{field.date}}` or relative `+3d`, `+2h`), duration, all day, location, time zone, registration. A following message step sends invitations with the stored address |
| Further workflow | starts a case of another workflow with values; if desired waits until it has ended, then takes over values and branches on failure |
| Parallel branches | starts several branches at the same time – fixed steps or **one branch per value** of a field (e.g. one review per department, the value is `{{field.item}}`); every branch ends at the **meeting point** (next step). Optionally *k* of *n* branches are enough, the others are then ended; values of the branches are taken over |
| End | closes the case with a result (successful, not successful, neutral) |

Automatic steps with permissions (HTTP request, create page, properties, create file, calendar entry, permissions, group membership, move page, further workflow) act with the permissions of the person who saved the workflow (or of the starting person) – nobody gets more through a workflow than this person may do themselves.

For tasks and approvals a **reminder after X days** can be set. Those responsible receive an e-mail for new tasks (can be switched off in the profile).

**Deadlines:** tasks and approvals can have a deadline – days/hours from reaching the step or from a date of the form, if desired only **working days** (without weekends and the public holidays under *Administration → Workflow*). Before it expires, those responsible are warned (e.g. 48 and 4 hours before). If the deadline expires, the chosen **escalation** happens once: only a message, hand over to someone else, decide automatically (an action or approval/rejection) or cancel the case – each optionally with a message.

**Form of a step:** fields with *Filled in later* do not appear in the start form. For tasks and approvals, *Form of this step* sets which fields those responsible fill in (optional or required), only see or **do not see** (e.g. the salary). Whoever started the case, managers and admins always see everything.

**Handing over tasks:** for tasks of a group or role, a person can **take over** the task (the others then no longer see it) and **give it back**. Every responsible person can **pass it on** (to someone who may see the workflow) or ask the **applicant a question**: they answer with a note, then the task comes back. Everything is in the history.

**Deputy:** under *My tasks* you enter a deputy with a period for an absence. During this time they complete your tasks (also those of your groups and roles) and receive the task e-mails; the log records their name.

**Deciding from the e-mail:** if *Decision links in task e-mails* is on in the section *Options* of the workflow, every task e-mail contains one link per decision (e.g. *Approve*, *Reject*). The link opens the case with this decision; it only applies to the person the e-mail went to, only to this step and only once. How long it is valid is set by *Links valid for* (hours or days, default 14 days, at most 90 days); if the step has a deadline, the link ends with it at the latest, and as soon as the case moves on it is invalid anyway. If the administration allows *Decisions from e-mails without sign-in* (under *Administration → Workflow*), the decision can also be confirmed without signing in – never in confidential or secret categories.

**Signed decisions:** for tasks and approvals, *Signature of the decisions* can require that every decision is signed – with any method, only qualified or only with a certain method; single actions can differ (e.g. "Not responsible" without a signature). When deciding, you choose the method (today: passkey or security key with PIN or biometrics, to be set up in the profile) and sign exactly this decision with it: action, note, values of the step form and all data of the case at this moment. Afterwards the history shows *signed – verify*: the check shows whether record, signature of the person and seal of the wiki are correct; the export (JSON) can be checked elsewhere, on the page *Verify signature* (`/signatures/verify`) or with `ewiki signature verify FILE`. Decision links from e-mails always lead into the case when a signature is required.

**Signed e-mails (S/MIME):** under *My profile → S/MIME certificate* you can upload your own S/MIME certificate (PKCS#12 file with password) if the administration allows it. E-mails the wiki sends with your address as the sender (e.g. from workflows) are then signed with it. The certificate must contain one of your confirmed addresses. For this, the private key is stored encrypted on the server: this is proof of the sender, not an electronic signature under your sole control. Every use is logged; you are notified before it expires and can remove the certificate at any time.

**Test run:** whoever may edit the workflow starts a trial run with *Start test run*. All tasks go to the testing person, nothing leaves the wiki (no e-mails, messages, webhooks, HTTP requests or new pages – noted as *skipped* in the history). Test runs are in the filter *Test runs*, do not count in the evaluation and are deleted after 7 days.

**Versions:** every case remembers the version of the workflow it was started with (shown in the case) and runs to the end with it – even if the workflow has been changed in the meantime.

**Evaluation:** whoever may edit the workflow or manage cases sees under *Evaluation* open, completed and cancelled cases, results, average and median duration, overdue cases, escalations, versions used and per step how many wait there and how long they stay on average.

**Retention:** under *Options*, *Keep completed cases (days)* sets how long ended cases stay. Afterwards they are **anonymised** (title, data, notes and people removed; statistics stay) or **deleted**. Empty means unlimited.

**Messages per step:** every step can send messages when a case reaches or leaves it (in the wiki, by e-mail or both, with placeholders).

**Triggers** (section *Start*): cases do not only start with the start form, but also automatically – if desired *only* through triggers:

| Trigger | Starts a case when … |
|---|---|
| Schedule | the time comes: `minute hour day month weekday` (e.g. `0 7 * * 1-5` on working days at 7:00) or `@daily`, `@weekly`, `@monthly` |
| Changes of pages | pages of the chosen sources are created, changed, deleted, moved, restored or get new properties – optionally only when a property **takes on** a value (e.g. *Status* becomes *Review*; once per change) |
| Event | a comment was written, a form submitted, someone registered for or deregistered from an event or a case of another workflow ends (chaining, optionally only with a certain result) – if desired only in one category |
| Accounts | a person was created, disabled, enabled again or deleted or added to or removed from a group (optionally only one group) – no matter whether through the administration, SCIM or a sign-in provider. Only for people who manage accounts (`core.users.manage`); values: `{{trigger.data.username}}`, `display_name`, `email`, `group_name` (on- and offboarding) |
| Incoming webhook | another system sends a signed request (see below) |

Title and values of the start form come from placeholders of the trigger: `{{trigger.object.title}}`, `{{trigger.object.url}}`, `{{trigger.object.id}}`, `{{trigger.actor}}`, `{{trigger.time}}`, `{{trigger.data.KEY}}` (values of the event or of the JSON of a webhook, e.g. `{{trigger.data.order.id}}`). The editor shows per trigger whether it is active, the starts of the last 24 hours, the next run and the last error.

**Security:** automatic steps (HTTP, page, properties, further workflow) and triggers act with the permissions of the person who **saved the version of the workflow** – never with the permissions of an admin who only may edit the workflow: if they save, their own permissions count. Alternatively steps act with the permissions of the starting person and triggers of changes and events with those of the person who caused them. This person must be allowed to see the workflow and the page or the event, otherwise nothing starts; sources are resolved with their permissions. A trigger starts at most 60 cases per hour (expert value `workflow.process.trigger_per_hour`), chains of workflows end after 5 links. Values a trigger takes over into the case are seen by everyone who sees the case.

**Incoming webhook:** after saving, the trigger shows its address; *Generate secret* delivers the secret **once** (it is not part of the workflow, a new one makes the old one invalid). The other system sends `POST` with JSON and the headers `X-Ewiki-Timestamp` (Unix time) and `X-Ewiki-Signature: sha256=<hex HMAC-SHA256(secret, timestamp + "." + body)>`, optionally `X-Ewiki-Delivery` (an ID – every delivery starts only once). Requests older than 5 minutes, with a wrong signature or (if set) from senders that are not allowed (addresses, networks, host names) are rejected with `403`; accepted with `202`. Example:

```sh
BODY='{"order":{"id":"A-17","item":"Chair"}}'
TS=$(date +%s)
SIG=$(printf '%s.%s' "$TS" "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | sed 's/^.* //')
curl -X POST "$ADDRESS" -H 'Content-Type: application/json' -H "X-Ewiki-Timestamp: $TS" -H "X-Ewiki-Signature: sha256=$SIG" -d "$BODY"
```

**Placeholders** in e-mails, files, titles, calendar entries and all other text fields of the steps: `{{field.<id>}}` (every field separately, e.g. `{{field.email}}`; also values that steps set), `{{label.<id>}}` (label of the field), `{{all}}` (all fields with label, one per line), `{{case.title}}`, `{{case.id}}`, `{{case.url}}`, `{{case.step}}`, `{{workflow.title}}`, `{{starter.name}}`, `{{starter.email}}`, `{{actor.name}}` and `{{actor.email}}` (who last worked on the step), `{{last.action}}`, `{{last.note}}` (action and note of this person), `{{site.name}}`, `{{date}}`, `{{case.due}}` (deadline) and all values that steps set. In the editor, a click on a placeholder inserts it into the field used last. As recipients, `starter`, `actor`, `user:name`, `group:name` and `role:name` are also possible.

Templates: *HR: application* (with acknowledgement of receipt and rejection letter by e-mail), *Approval request* (approval) and *Onboarding* (waiting time until the first working day, welcome e-mail).

On the page, *Start new case* starts a case. The table shows the cases you may see; "Your turn" marks where you can act. A click on the title opens data, history and actions. **My tasks** (sidebar or `/tasks`) collects everything waiting for you. Running cases keep the process they were started with, even if the page is changed later.

### Translating
In edit mode, every page shows what percentage of it is translated and the status for every language:

| Status | Meaning |
|---|---|
| Original | language in which the page was created |
| Current | translation is based on the newest version of the original |
| Outdated | the original was changed since the translation |
| Missing | no translation yet |

With *Translate* or *Edit* you open the editor for this language. Under *Show original* you see the source text.

You translate **categories** the same way: in edit mode the button **Translations** (with the permission *Manage*) shows the status of title and description per language and the progress. *Translate* shows the original for comparison; an empty title removes the translation. If the original has changed, the translation is *outdated* until you revise it or confirm it with *Still correct*. The **original language** of a category is the language in which you created it; it can be changed under *Edit category*.

### Forms
With *New page*, choose the element type **Form** (permission *Create forms*). The editor has the **field types** on the left – a click (or Enter) adds a field at the end, and with the mouse they can also be dragged to a place in the field list. You order fields with ↑/↓ or by dragging the handle ⠿; a click on a field opens its settings:

- **Field types:** text, long text, e-mail, phone, web address, number, date, time, date and time, selection list, multiple selection, options, checkbox (one or several), rating (stars), slider, **computed value**, **repeating group**, file upload, **signature**, consent, hidden value as well as heading, text block, horizontal rule and **New page**.
- **New page** splits the form into steps: senders see "Step 2 of 3", *Next* checks the fields of the step, *Back* goes without checking; after an error on submitting, the form jumps to the affected step.
- **Computed value:** a formula of numbers, `+ - * /`, brackets and `round(x, digits)`, `min`, `max`, `abs`, `sum` with the IDs of fields above (checkbox = 1, choice with number values). The value appears while typing and is computed again on the server on submitting.
- **Repeating group:** rows with several fields (text, number, date, choice, checkbox …), e.g. "Guest: name, meal"; *add*/*remove* by button, minimum/maximum rows.
- **Signature:** draw with mouse, pen or finger – or type the name (keyboard, screen reader); the drawn signature is stored as PNG.
- **Settings per field:** label, ID (for placeholders and exports), help text, required field, example in the field, default value, prefill (name, e-mail or user name of the signed-in person, value from the address `?name=`), minimum/maximum length, pattern with explanation, minimum/maximum/step, autocomplete (WCAG 1.3.5) and **Only show if** a field above has a value (is, is not, is empty, is filled, contains). Hidden fields are neither checked nor sent.
- **File upload:** allowed kinds (images, PDF, office, text/CSV, ZIP, audio, video) or extensions, number and size per file, comment per file, optionally additionally into a category of the media library. Senders choose files or drag them onto the upload area. Every file is checked by its **content** (does it match the extension?), office files with **macros** and PDF files with **scripts, actions or embedded files** are rejected (can be allowed per field), a configured **virus scanner** checks additionally.
- **Layout:** *Grid* (recommended) – every field takes 1 to 12 columns, on narrow screens below each other. *Freely placed* – fields are dragged on an area and resized at the corner (keyboard: arrow keys move, with Shift the size, with Alt in 1-pixel steps; plus number fields). Screen readers and keyboard always follow the order of the field list; on small screens the grid applies.
- **Submitting:** only signed-in people, once per person, period (opens/closes), maximum number, delete after days, copy to the sender, **arithmetic task against bots** (the browser computes for about a second, no CAPTCHA), **results public** (frequency of the answers for everyone who sees the form).
- **What happens with a submission:** keep in the wiki, **e-mail** to addresses, `user:NAME`, `group:NAME` or `field:ID` (an e-mail field) with subject and text including placeholders, **webhook** (JSON, signed), **entry in a property database** (fields → properties), **start workflow** (fields → fields of the workflow), **store as file** (PDF, JSON, CSV or text in a category of the media library, file name with placeholders). External e-mails and webhooks need the permission *Send submissions outside*. These targets are executed with the permissions of the person who created the form – if they lack a permission in the meantime, the target is not executed and shown as an error with the submission. No content leaves confidential areas by e-mail or webhook, only the link.

**Every person who may see the form may submit it** (also guests if the category is public). Errors are at the field and collected at the top with links to the fields; the focus jumps there. A hidden field, a minimum time and limits per address or account help against automatic submissions – without CAPTCHA.

**Submissions** are seen by the person who created the form and everyone with *View submissions* below the form: search, files for download, delete, export as CSV or Excel (values starting with `=` stay text). A link in the e-mail leads directly to the submission. The macro **Form results** (`::form-results{form=ID}`) shows the frequency of the answers as a table with bars (with public results for everyone, otherwise only with *View submissions*) or with `show=table` the submissions as a table (only with *View submissions*), with maximum and pages.

### Blog, FAQ, event calendar and team

When creating an element (*New element*) you choose the type:

- **Blog**: shows the pages of a category (optionally with sub-categories) as posts, the newest first. There are teaser text, author and date and the layouts list, cards and compact, plus paging, tag filter (click on a tag) and a **search only in the blog**. The posts are normal pages: **New post** opens the editor in the blog category, **New sub-category** creates e.g. "Events". In the editor of the blog you choose the category or create it with **New category**. Under **Further sources**, posts of other categories, tags or element types are added (also in the feed and in the search of the blog).
- **Shared notes**: a page on which everyone with edit permission writes directly in the view – at the same time, with the cursors of the others (live editing). It is saved automatically a few seconds after a change (at most one new version every 30 seconds, in the history as "Notes saved automatically"). Readers see the notes as a normal page. Ideal for meeting minutes and shared task lists.
- **FAQ**: sub-categories become expandable sections, pages expandable questions (title = question, content = answer). The search field filters immediately, *Expand/collapse all* helps with reading. **New question** creates question and answer directly in the dialog (Markdown), **New section** a sub-category. Every question has its own link (`#q-123`). **Further sources** add questions of other categories as their own sections.
- **Event calendar**:
  - Views month, week and list, categories with colours, search.
  - Recurring events: daily, weekly on certain days, monthly on day X or e.g. "on the last Friday", yearly; ending after a date or a count. Single events of a series can be cancelled or changed with **Edit only this event** (title, location, description, date and time). Changed events are marked, registrations stay, and registered people receive an e-mail on a new time or location. **Reset change** restores the event as in the series; **Edit series** changes all events.
  - **Registration** with number of places, waiting list and registration deadline. Confirmations come by e-mail with a **calendar invitation**: Outlook, Google or Apple Calendar enter the event directly, update it on changes and remove it on deregistration or cancellation (can be switched off per calendar). If a place becomes free, the first person on the waiting list moves up automatically and is informed.
  - **Reminders by e-mail** (e.g. 1 day before) to registered people and everyone who has chosen "Remind".
  - **Add to my calendar (.ics)** for an event and **Subscribe** to the whole calendar in Outlook, Google or Apple Calendar (personal link). Organisers see the participants and can export them as CSV.
  - **Further sources**: events of other calendars (only those the readers may see; they are changed in their calendar) and **iCal subscriptions** of external calendars (e.g. public holidays; updated every 10 minutes, read-only, the description as plain text). Every event shows which calendar it comes from.
  - The macro **Upcoming events** shows coming events on every page – from this one, from several calendars or from iCal subscriptions.
- **Team**: name and description (Markdown/HTML), one or more groups whose members are shown, and the selection of the data: photo, function, department, e-mail, phone, mobile, office, "About me", groups or attributes from LDAP/OIDC/SAML. Shown as cards, list or table, if desired structured by groups, with filter and CSV export. Every person maintains the data under *My profile → Business card*, administrators under *Administration → Users*.

### Reactions and favourites
- **Reactions:** below pages and categories as well as on comments you react with an emoji. *Add reaction* opens a selection of common emojis (arrow keys choose, Esc closes); through *Other emoji* any emoji works. A click on a reaction switches your own on or off; who reacted is in the tooltip and is read out by screen readers. The author learns about it in the wiki (by e-mail only if they switch that on in the profile).
- **Favourites:** *Favourite* on a page or category bookmarks it. The list *My favourites* is in the sidebar (or at the position chosen by the administration) and in the profile, where it can also be sorted and items removed.
- **Properties:** see below.

### Properties, databases and reports

Every element – page, file, blog, calendar, no matter which type – can have **properties**: data such as status, responsible person, due date, budget or location.

- **Editing:** in the editor of every element there is the area *Properties* below the tags; on the page in edit mode it also works faster with *Edit properties* (without a new version). Required fields are marked with *, the wiki sets computed values (sequential number, formula) itself. *History* shows who changed which value when.
- **Defined and free properties:** the management of a category sets properties with type, choice values, default values and permissions (*Category settings → Properties*; global ones under *Administration → Properties*); they also apply to all sub-categories. Where allowed, you add your own with *Add property* (name and type).
- **Types:** text (single and multiple lines), e-mail, phone, link, colour, code/identifier, language, country, whole number, decimal number, percent, amount of money, number with unit, rating (stars), progress, duration, yes/no, date, date with time, time, period, choice, status (coloured), multiple choice, person, group, linked elements, category, file, location, version, sequential number and formula. **Tags** are the standard property of every element.
- **Property database** (element type): all elements of one or more sources as a table, cards, board, calendar, timeline or chart – search, filter, group, change values of a row directly, *New entry*, download as CSV or Excel. **Saved views** remember search, display, grouping, filter and sorting – for yourself or (with edit permission) for all readers of the database.
- **Macro "Property report":** in the macro dialog choose sources (categories with/without sub-categories, tags, element types, single elements), filters with matching conditions per type (e.g. *Due date at most today+30d*, *Responsible is me*), columns, sorting and display: **table** (with groups, subtotals and totals), **list**, **cards**, **board** (move cards into another column by selection field or dragging), **calendar**, **timeline**, **chart** (columns, bars, stacked, line, area, pie, donut, heatmap), **key figures**, **pivot table**, **tag cloud**, **completeness** (which values are missing), **overdue and due soon** and **locations**. Readers can set their own filters, download as CSV or Excel and **subscribe** to the report (daily or weekly by e-mail, always with their own current permissions; confidential entries only with title and link). The display **Trend** shows from the change history how a choice, status, yes/no or number property has developed week by week (e.g. open and completed tasks, sum of hours). Strictly confidential entries are never contained in downloads and e-mails. Charts have a text summary and the data as a table.
- Everything only shows what you may see: elements without read permission and properties with restricted visibility are also missing in filters, totals and charts.

### Addresses and "Page not found"
Every page and category has **a readable address per language** from its title, e.g. `/wiki/unser-team/ueber-mich` and `/wiki/our-team/about-me`; without a translation the address of the original language applies. All language addresses work, the address bar shows the address in your language. With **Only this short URL** (at the short URL in the properties) a page can only be reached through its short URL. If a page is renamed or moved or a short URL changed, the **old addresses still lead to the target** (permanent redirect). If an address leads nowhere, the wiki shows whether the page was deleted (with a link to the trash for everyone who may restore) and suggests matching pages ("Did you mean?") – only those you may see.

### Short URLs

Elements and categories can get a **short URL**: pages through *Properties* (there also the **preview image** that appears when sharing a link in Teams, Slack or Outlook – without one, the first image of the page), categories through *Edit category* or the settings. With `calendar` the element can then be reached under `https://your-wiki/calendar`. Sub-levels such as `team/hr` are possible. Every short URL exists only once; words the wiki uses itself (`admin`, `login`, `search`, …) are blocked. Menus and overviews link the short URL automatically.

### Start page and category overview

Under *Administration → Settings → Start page*, administrators choose what appears under `/`: the overview of the top categories, a category or an element. New installations show the welcome page. The element type **Category overview** is suitable for an overview of areas: one category or the top level, with 1–5 levels of sub-categories, as cards or list, if desired with descriptions and the pages of every category.

### Areas
Every top-level category is an **area**. Whoever has the permission *Manage* there is area management – without any administrator permissions – and sets in *Category settings → Area*:

- **Area key** (e.g. `HR`): the area can then be reached under the short address `/s/HR`; keys are unique.
- **Own navigation**: within the area the sidebar only shows its categories, with name and key of the area at the top.
- **Shortcuts**: important pages or external addresses at the top of the navigation of the area.
- **Allowed element types**: only these types can be created in the area or moved there; *New element* only offers them.
- **Language of new elements**: default language for new pages in the area.
- **Allowed macros**: the editor only offers these macros in the area; others are not executed in the area, but shown as a notice.
- **Logo** and **accent colour**: logo in the navigation and in the directory; the accent colour applies to links and buttons in the area and must have a contrast of at least 4.5:1 with white.

*All areas* (`/spaces`, link at the bottom of the navigation) lists all areas you may see, with a filter. Name and shortcuts of an area are only seen by whoever may see its top category.

**Page templates of an area:** with *Save as template*, area management chooses "Everyone in the area …". Such templates are only offered when creating pages in this area.

**Archiving** (*Category settings → General → Archive*): the category with everything below becomes read-only for everyone – also for administrators –, disappears from navigation, overviews and search and shows a notice. Direct links keep working; archived areas are listed under *All areas* in their own section. *Restore* makes everything editable again.

**Area templates:** category administrators can *offer an area as a template for new areas*. *All areas → New area* then creates an area with sub-categories, permissions, settings, website template and page templates of the template – if desired also with the pages.

**Permissions of an area** are the completely normal permissions of its top category (*Category settings → Permissions*) and therefore also appear in *Administration → Permissions* – areas are marked there. Whoever has permissions as a person, through a group or a role is a **member**; *All areas → Only my areas* shows these.

**Moving:** every category can be moved at any time. If it becomes top level, it is an area (earlier area settings apply again, the key must be assigned again). If an area is moved below another category, it releases its key; its settings and page templates rest, and those of the new area apply. Moving and copying into an area is only possible if its allowed element types match.

**Requesting access:** whoever may not see a page or category gets "Not found" (whether it exists is not revealed) and, when signed in, can *Request access*. The people who manage the category receive a notification and answer open requests under *Category settings → Permissions* with *Allow reading*, *Allow editing* or *Reject*; this becomes a normal permission rule. At most 10 requests per person and day.

**Personal area:** with the permission *Create personal area*, every person creates their own area under *All areas*, which only they see until they share it through the permissions.

**Classification** (*Category settings → General → Classification*): *Public*, *Internal*, *Confidential* or *Strictly confidential*; it applies to everything below, and the strictest classification above always applies (a sub-category can only become stricter). Consequences:

| Classification | Effect |
|---|---|
| Internal | never for guests – even if a rule allows guests to read; notice "Internal" on every page |
| Confidential | additionally not in Atom/RSS feeds and calendar subscriptions, watch e-mails only with title and link, notice on every page; images and PDFs of the media library as well as images in pages carry the name of the viewer and the time as a watermark |
| Strictly confidential | additionally not in the general search (only when searching within the category), not in external search indexes, no export, no copies; files of the media library and file elements can only be viewed, not downloaded (except with manage permission) |

**Protection of files selectable:** in the *Category settings*, below the classification, there are *Watermark for files* (*Automatic* or *Off: always the original*) and *Downloading files* (*Automatic* or *Allowed: download the original*). Without a choice the setting above applies; a sub-category can switch the protection on again.

**Settings of a file take precedence:** what is set directly on a file of the media library – own permissions, *View only* or a watermark – cannot be lifted by settings of the category or the area. Category and area can only restrict additionally. Such files are also not delivered without these restrictions through a page that embeds them.

**Downloads of reports and databases:** selectable when creating or editing: *Safe* (default: values with `=` at the start stay text, so Excel does not execute anything unexpected) or *Original* (such values become formulas – only for trustworthy content).

**Evaluation** (*Category settings → Evaluation*, with *Manage*): views per day, most-read pages, pages not changed for over six months and contributors of the last 7 to 365 days. Views are counted sparingly only per page and day – without people and addresses – and deleted after 400 days.

**Summary of an area:** *Watch* on the top category with sub-categories and a daily or weekly summary sends all changes of the area collected by e-mail.

**Exporting and importing** (*Category settings → Area*): the export is a file (JSON Lines) with all sub-categories, pages (last version of every language), settings and page templates. *All areas → Import area* creates a new area from it, *Import here* a sub-category. Permissions and files are not contained (they depend on the installation); all content is checked and sanitised on import as on saving, entries that cannot be taken over are listed.

### Own website template per category
In the *Category settings*, whoever may manage the category chooses one of the active templates under **Website template** (e.g. *CMS* for a public area within an internal wiki). It applies to the category, its sub-categories and all pages in them; a sub-category can choose its own again (the nearest wins). *Inherit* uses the template above or that of the site. When changing between areas, the template changes without reloading; content, focus and permissions stay the same.

### Carousels
Carousels show at a position of the template (e.g. above the content) news, own slides, entries of external feeds and the newest elements of any **sources** (categories, tags, element types – with title, summary and preview image; every person only sees what they may read). With **Publish with options**, with the permission *Publish news in carousels*, you choose "As news in …" and optionally an end date; news is only seen by people who may see the page. With *‹* and *›* or the dots you change the slide; if the carousel runs automatically, *Pause* stops it (it also stops as long as mouse or focus are in it, and does not run at all if your system requests reduced motion).

### Menus

Under *Administration → Menus* (administrators), menus consist of entries with a target:

- a **category**,
- an **element** of any type (page, blog, FAQ, calendar …),
- a **free link**,
- **another menu** as an expandable sub-menu (for multi-level menus),
- a **heading** that only groups the indented entries below it.

You choose categories and elements from the existing ones: categories through a filterable list, elements through a search by title or short URL in all categories. The category only serves as an optional filter. With **New category** or **New element** you create targets directly. For a new element only type and title are needed. The suggested storage category only determines who may see and edit it. For blog and FAQ, a category with the title is created automatically if no source is chosen. If the label stays empty, the title of the target appears in the respective language. Entries someone may not see are not shown to them.

When editing a menu you only see the list of entries with target. **Edit** opens exactly one entry (target, labels, parent entry up to four levels), **Move up**/**Move down** change the order, **Add entry** creates a new one. Only **Save** in the menu dialog applies the changes.

Menus without a place ("only as sub-menu") can be hooked into several menus. In the header, sub-menus open as drop-down menus (by mouse, keyboard, Esc closes), in the sidebar as an expandable tree, in the footer completely.

## Watching

With **Watch** (on every page and category, for signed-in people) you receive an e-mail when something changes, for categories if desired also for all sub-categories. You choose whether the e-mail contains **only the link** or **link and new content**. Several changes in quick succession come collected in one e-mail. Instead of **immediately** you can choose a **daily** or **weekly summary**; it comes at the time set by the administration (default 7 am, weekly on Mondays). Your own changes are not reported, and only content you may see is reported. You manage your watches under *My profile → Watches*.

The administration can also **assign** watches to you (e.g. for a department). They are under *My profile → Assigned watches*; you can unsubscribe from optional ones there, not from mandatory ones.

## Notifications, sharing and @mentions

The **bell** next to your name shows notifications in the wiki, the number of unread ones is next to it. A click on a notification opens the target and marks it as read; *Mark all as read* empties the list of unread ones.

- **Share** (button on every page) points people, groups or roles to the page with an optional message. Sharing gives **no permissions**: whoever may not see the page is not notified – *Check recipients* shows this beforehand.
- **@mentions:** type `@` and the name in a page or a comment; the mentioned person receives a notification if they may see the page.

In addition an e-mail comes each time, as long as you do not switch it off under *My profile → E-mail notifications*.

## Feeds (Atom/RSS)

In the *Category settings* an **Atom/RSS feed** can be switched on (only this category or with sub-categories). Blogs and FAQs have their own feeds. The button **Feed** shows the addresses for feed readers. Signed-in people get a **personal link** with a key, so the feed reader can also read protected content. Under *My profile → Feed and calendar links* you renew or delete the key if a link was passed on.

## E-mail notifications
Under *My profile → E-mail notifications* you set what you are informed about by e-mail: new tasks and reminders, completion of your cases, comments on your pages and answers in discussions as well as changes to watched content. This requires an e-mail address in the profile. If sending e-mail is switched off by operations, the profile points this out.

## Comments
Below every page, signed-in users can comment and answer comments. Your own comments can be edited and deleted. Links are linked automatically.

## History and restoring

Every save creates a new version. Nothing is ever overwritten. Under **History** you can view every version and **restore** it. This creates a new version with the old content.

You reset a whole category to a **point in time** under *Category settings → History & reset*, optionally with all sub-categories. Titles, texts and pages get the state of that time, pages created later go into the trash. This too can be undone again.

## Trash

Deleted categories (with all content) and pages end up in the **trash** (user menu). From there you restore them. They are only deleted permanently by administrators or automatically after the set retention time.

## Search and tags

- The **search** finds words and word beginnings in titles, tags and content of all languages. Hits are highlighted.
- You assign **tags** to categories (settings) and pages (*Tags*). A click on a tag shows all content with this tag. On the search page there is a list of popular tags.

## Permissions of a category

With the permission *Manage* you set under *Category settings → Permissions* who may do what: user, group, role, all signed-in people or guests. Choose a template (**Reader**, **Category editor**, **Category manager**) or single permissions, also the permissions of plugins (e.g. *Create workflows*). Optionally a rule also applies to all sub-categories. Inherited rules are shown with their origin. Details: [docs/en/admin/security.md](../admin/security.md#permissions).

## Signing in

Depending on the setup you sign in with user name and password (local account or LDAP/Active Directory) or with a button **Sign in with …** through the central login of your organisation (OpenID Connect or Shibboleth/SAML). If you have set up further factors (or the administration requires them), the form then asks for them step by step, e.g. for the code from your app; if you lack a required factor, you set it up right there. With a **passkey** you sign in, where allowed, completely without a password.

## My profile

- Display name and preferred language, **e-mail addresses** (several possible, one of them the main address) and **linked sign-ins**,
- change password (local accounts), optionally enter a **user name** and under **Sign-in factors** see your own factor chain and set up factors: authenticator app, code by e-mail, **security key** or **passkey**, **allowed networks**,
- **Sessions**: where you are signed in; end single sessions or "Sign out everywhere else",
- **API keys** for scripts: choose permissions, category and expiry date. The key is only shown once,
- **SSH keys** for passwordless sign-in to the API,
- **Business card** (photo, function, department, phone, office, "About me") for team pages. Who sees your photo (only you, signed-in people or everyone) you choose within the limits set by the administration,
- **Watches**, **assigned watches** and **feed and calendar links**,
- **E-mail notifications** (also for mentions and shared pages).

## Accessibility

The interface can be operated completely by keyboard (Tab, Shift+Tab, Enter, Space, Esc closes dialogs and menus). With **Skip to content** (first Tab key) you skip the navigation. All controls are labelled for screen readers, status messages are announced, and after a page change the focus jumps to the heading.
