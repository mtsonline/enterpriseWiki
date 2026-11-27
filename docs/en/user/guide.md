# User guide

## Structure

- **Categories** organise the wiki like a table of contents. They can have any number of sub-categories and carry an outline number (e.g. `1.2.3`).
- **Elements** are in categories. The standard type is the **page** (formatted text or Markdown). Further types come through plugins, e.g. the notice box.
- Every category can have a **default page**. It is shown directly when the category is opened.

On the left you see the **page tree**. With the arrow buttons you expand and collapse sub-categories. At the top you find search, language selection, the **Edit** button and your user menu.

## Reading and language

Choose the **language** at the top. Interface and content change together. If a page does not exist in your language yet, you see the original language with a notice. Your choice is stored in the browser. Under *My profile* you can set a preferred language.

**Buttons with only an icon** (✎, ×, ☰, 🔔 …) show their name when the mouse rests on them, when they get the keyboard focus and – on tablets and phones – on a long press (the long press only shows the name; a short tap presses the button). Escape hides the name.

**On phones and tablets** (up to 1100 pixels wide, or up to 520 pixels high, as on a phone held sideways) the page starts with its content. Two buttons at the top open what is closed at first: the menu button ☰ opens the navigation with the side column – held upright across the whole width below the bar, held sideways as a column of the full height at the side with its own close button – and the button with the magnifier opens search, language, edit mode and user menu. Escape, a click beside the menu or following a link closes them again. Large blocks such as the carousel and sections take less height there, so more of the page is in view.

**Maintenance:** a bar at the top says when the wiki is read-only for maintenance – you can read everything, but saving, commenting and uploading are not possible until it ends. While the wiki is closed for maintenance you see only a message; administrators can still sign in.

**Consent:** if the administration asks for it, you accept the privacy policy after signing in (or sign out), and a banner asks which storage beyond the necessary one the wiki may use in your browser. You can change that choice at any time with *Cookie settings* (in your profile or where the administration placed it); the profile also shows when you accepted which version of the privacy policy.

## Edit mode

As long as edit mode is off, every page looks as it does for all readers. With the switch **Edit** (only visible if you have edit permissions; it shows *on* or *off*) you switch it on and off. Then the actions appear:

**In a category:** *New page*, *New sub-category*, *Edit category* (title, description and tags in all languages), *Category settings* (default page, address, permissions, history), *Move*, *Duplicate*, *Delete* and arrows for sorting the sub-categories. The **default page** of the category (the text shown directly in the category) has its own buttons *Edit page*, *Tags* and *History*; in the list *Pages*, *Edit* leads directly into the editor.

**On a page:** *Edit*, *History*, *Tags*, *Set as default page*, *Move*, *Duplicate*, *Delete* and the overview **Translations**.

### Drafts, preview, templates and publishing
In the editor, **Save as draft** stores the state without publishing it. Only you and the editors of the category see drafts; they are listed in the category under *Drafts*. **Preview** shows the page as readers will see it. If there are unsaved changes, the preview first saves them as a draft; links in the preview open in a new tab, and the editor stays as it is. If you leave the editor with unsaved changes (a link, *Cancel*, Back in the browser), the wiki asks: save as draft, discard or keep editing. When you close or reload the tab, the browser asks. **Save as template** stores the content as a template (for you, the category or – with the permission to do so – for everyone); with *New page* you choose a template. Under *Manage templates* you change the name and description of a template or replace its title, content and tags with what is in the editor now; you also delete templates there.

**Publish** applies the change immediately. **Publish with options** asks for the target category (only categories in which you may create), a point in time (scheduled publishing) and whether watchers are notified. Plugins can add further options, e.g. "as news" in a carousel.

**Block templates:** select blocks in a page (text, layout and wiki blocks with their options) and choose *Save as block template*: only for you, for a category and below, a space or for everybody (like page templates). *Insert a block template* puts a template at the cursor, also in Markdown mode. Once inserted it is part of the page: later changes of the template do not change the page. For content that should stay the same everywhere, use the block *Include page*.

### New sub-category
The currently open category is automatically selected as the parent category. You can enter title and description for every language.

### Editing pages
Pages are stored as **Markdown** (GitHub Markdown). There are two editors; you choose which one you use in your **profile** (*Editor for pages*), otherwise the default of the site applies.

**Visual editor** (default):
- Toolbar: paragraph format (paragraph, heading 1–4, code block), bold, italic, strikethrough, code, link, bulleted list, numbered list, **task list**, quote, table, image, horizontal rule, block, undo/redo. In a table, additional buttons for rows and columns appear.
- Markdown while typing: `# ` to `###### ` → heading, `- ` → list, `1. ` → numbered list, `> ` → quote, ` ``` ` → code block, `---` → horizontal rule, `[ ] ` at the start of a list item → task, `**bold**`, `*italic*`, `~~strikethrough~~`, `` `code` ``. Pasted Markdown text is converted.
- **Tasks with a person or a date:** a task item that names people with `@name` or has a date (block *Date*) is a task of the wiki, for example `- [ ] Check the offer @anna :date{value=2026-11-30}`. It belongs to all people named; one tick is enough. It appears in the *Report of tasks*, under *My tasks* (`/tasks`) and in the tile *Tasks in pages* of the dashboard. The people of the task tick it there without opening the page, if they have the permission *Tick own tasks in reports* (by default everyone with the template *Reader*); who may edit the page ticks every task. Ticking saves the page: it keeps its author, the history shows who ticked (“Task done: …”). If the page was changed in the meantime, nothing is ticked and you are asked to reload. Tasks inside blocks that show their content only to some readers are not listed. Without knowing how it is written: *Insert block → Task with person and date* (or type `/task`) asks for the text, the people (search by name, several possible) and the due date.
- **Blocks while typing:** `/` at the start of a paragraph or line, or `:` followed by letters (`:date`, `::tasks`, as blocks are written in Markdown), shows matching blocks and the task with person and date, in the visual editor and in Markdown. Arrow keys choose, Enter or Tab open the dialog of the block (its settings are filled in there), Escape closes the list and the text stays as typed. *All blocks …* opens the full menu.
- **Emoji:** the button **Insert emoji** (☺) opens a picker with a search in English or German, by the language of the interface, by name and keywords (for example “thumb” or “heart”), and with the groups of Unicode. The arrow keys choose, Enter inserts. The emoji is a normal character in the text. Names and keywords come from Unicode CLDR.
- **Code blocks** show their language in colour (more than 25 languages, for example Go, Python, JavaScript, SQL, YAML) and have a button *Copy code*. The language and line numbers are chosen in the code block with the button *Language and line numbers*, or in Markdown in the first line (` ```go lines `). The line numbers are only for the eye: copying and screen readers get the code alone. The colours always have at least 4.5:1 contrast.
- **Diagrams from text:** a code block with the language *Diagram from text (Mermaid)* (` ```mermaid `) is drawn as a diagram, for example a flow (`flowchart LR` with `A --> B`), a sequence, state, Gantt or class diagram. Below the diagram, *The diagram as text* opens the text, for screen readers and without script. If the text cannot be drawn, a note says so and the text is below. With `accTitle:` and `accDescr:` in the text you give the diagram a name and a description for screen readers.
- Keyboard: Ctrl+B/I/E/K (bold, italic, code, link), Ctrl+Shift+X (strikethrough), Ctrl+Shift+7/8/9 (lists), Ctrl+Alt+0…4 (paragraph, headings), Ctrl+Z/Ctrl+Y. **Alt+F10** jumps into the toolbar (arrow keys choose, Escape back into the text). **Tab** indents in lists and jumps to the next cell in tables; **Escape, then Tab** always leaves the editor. **Enter** on a selected block or image opens its settings.
- **Moving and sizing blocks:** drag a block by its head (⠿), or use ↑ and ↓ in its head, or **Alt+Arrow up/down** (moves the paragraph or block at the cursor; when it cannot go further, the block around it). The handle at the right edge sets the width: drag it, or focus it and use the arrow keys, Home and End. Widths go in steps (16 to 72 rem, or full width) and are saved as the option *Maximum width*, so they fit on every screen. The handle at the lower edge sets the *Minimum height* the same way (automatic, or 4 to 60 rem; arrow up makes the block taller). When a value is set per view under Options, both handles change only the value for the desktop; the values for tablet and phone stay.
- **Menu of a block:** right click on the head or the picture of a block, a long press on touch screens, or with the keyboard the context menu key or Shift+F10 (on a button in the head or with the block selected). The menu offers *Edit*, *Move up*, *Move down*, *Duplicate* and *Delete*; arrow keys choose, Escape closes. Ctrl+Z brings back what was deleted. In the text of a block, the browser's own menu stays (spelling, paste).
- **Markdown mode** (on the right of the toolbar) shows the source; the button then says **Visual mode** and switches back. The button always names the mode it switches to.
- **Live editing:** if several people edit the same page, everyone sees the changes and cursors of the others immediately. Above the text it says who is co-editing; coming and going is announced for screen readers. Undo only affects your own changes. If someone saves, the others get a notice and continue saving without a conflict. Every person has their own combination of colour and shape (●, ■, ▲, ◆); the same marker is at the cursor and in the list of co-editors above the text. Live editing happens in the currently active view – visual or **Markdown source** (also there with the cursors of the others). Whoever switches the mode switches for everyone; this is announced, and the focus stays where it is for every person.

**Simple editor:** a lean input field (formatted or Markdown) without live editing, e.g. for older devices.

**Hint on long sentences** (if the administrators offer it): while you write, the editor shows sentences with more than 20 words and words with more than 22 letters (the administrators may set other limits). A hyphen splits a word. Code, code blocks, addresses and paths do not count. The visual editor and the simple editor mark the places with a line under the text; the Markdown source only lists them. Below the editor a line sums up what was found, for example “Readability: 2 long sentences, 1 long word.” **Show places** opens the list; a place selects its text. The hint never changes your text and never stops you from saving. Screen readers hear the summary when the editor gets the focus, not at every key. You switch the hint on or off in your profile.

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

In the **detail view** you see size, versions and on which pages the file is used; with edit permission you change texts, upload a new version or open the **image editor** (crop, rotate, flip, scale, brightness/contrast/saturation, greyscale, blur, **make unreadable**). Every edit is a new version; all pages automatically show the newest, earlier versions can be restored.

**Make unreadable** (faces, number plates, names): mark the area on the picture as for cropping and choose *Fill black* or *Coarse blocks*; neither can be reversed (blurring partly can, so it is not offered for this). Older versions still contain the original. Whoever manages the file deletes them with *Delete older versions* in the detail view, which also offers it right after saving. This removes their content from the wiki for good and is recorded in the audit log; content that another file or page uses as well stays for that one. Backups keep it until they are rotated.

**Office files** (texts, spreadsheets, presentations) open directly in the browser with *Edit in …* or *View in …* if the
administration has set up an office program (Collabora, OnlyOffice or Microsoft 365). With edit permission every save
becomes a new version; without download permission you see the file read-only, without downloading, printing and
copying. If "always open directly" is set, a click on the file starts the program immediately.
*Close* leads back into the wiki.

### Images in pages
In the editor, the button **Insert image** opens the media library: choose an image or upload a new one (default: category of the page) and enter an alternative text that describes the image for people who cannot see it. Other files are inserted as a link. The block **Image gallery** shows selected images (`media="12,13,14"`) or all images of a category as a grid; a click opens the image large, with the arrow keys you browse on. A picture shown smaller than it is opens large with a click (as in the gallery); the link carries the alternative text.

### Blocks
Blocks are filled in when the page is shown (in the code and in Markdown they are called macros). In the visual editor the button **Insert block** (⚙) opens a menu: a search field at the top, below it the blocks in groups (*Text and emphasis*, *Structure and layout*, *Buttons and navigation*, *Media and people*, *Lists and reports*, *Figures and charts*, *Forms*, *Connections and automation*) and the **block templates**. The search finds blocks by name and description, across all groups. With the keyboard: arrow down goes from the search field into the list, arrow right opens a group, arrow left closes it, Enter inserts. Screen readers read the name of a block, and its description only when the choice stays a moment. With text selected, *Save as block template* in the same menu saves it as a template.

Blocks with content (e.g. notice box) wrap selected paragraphs. An inserted block appears as a dashed box with name, settings and the buttons *Edit block* (✎) and *Remove block* (×); blocks in the text (e.g. status label, text colour) as a small box. Layout blocks without content (e.g. button group, counter, icon box) and wiki blocks such as page tree or tag cloud show in the box how they look on the page, without their settings as text; the picture is drawn again when you change the settings with *Edit block* (✎). **Blocks with parts** (accordion, tabs, columns, cards, grid, timeline, content slider) start with two parts: the heading of a part is its title, the content goes below it. With **+ Part** (or *+ Tab*, *+ Column* …) at the top of the block you add another part; between two parts there is a labelled horizontal rule (it can also be inserted with the button *Horizontal line* or `---`). Some blocks have their own button in the toolbar (◉ status label, **A** text colour and font); the selected text is taken over.

In the Markdown source, blocks are directives: `::toc{levels=3}` (without content), `:::panel{type=info title="Attention"}` … `:::` (with content; nested blocks with more colons on the outside) and `:status[DONE]{color=green}` (in the text).

| Block | Effect |
|---|---|
| Table of contents | lists the headings of the page with anchors |
| Sub-pages | sub-categories and pages of this (or another) category |
| Recently changed | the most recently changed pages |
| Notice box | info, note, tip, warning or success, with title |
| Expand section | content that only appears after a click |
| Status label | coloured label such as DONE |
| Text colour and font | text colour, highlighting, font and size from the palette of the site (all combinations easily readable) |
| Columns / cards | content side by side; separate columns or cards with a horizontal rule. Cards stand in at most as many columns as chosen, each at least 12 rem wide: when the content area is narrower (beside a side column, on a tablet), they wrap to the next row |
| Section, hero banner, button link, spacing | building blocks for website pages (especially with the template *CMS*). The hero banner has a **height** for desktop and tablet and one for the phone: *as high as its content*, *a third* or *half of the window*, or *the window below the header* (`height=half phone_height=window`). It is always a least height: when the text is longer, the banner grows; the text is never cut, also not on a picture or video. |
| Grid | content in columns, the number per view (desktop up to 5, tablet up to 3, phone 1); separate the cells with a horizontal rule. With *Widths* the columns get their own shares on the desktop (`widths="2 1"`: the first twice as wide); the narrowest keeps enough room |
| Heading | a heading with subtitle and line; its level (the structure of the page for screen readers) is chosen apart from its size |
| Divider | a line (solid, dashed, dotted, double, fading) or space between parts of a page |
| Buttons | up to four links as buttons, side by side, over the full width or one below the other; each optionally with an icon in front of the text (decoration: the text names the button) |
| Call to action | a box with title, text and a button, optionally with an icon |
| Tabs | parts as tabs (arrow keys, Home, End); separate the parts with a horizontal rule, the first heading of each part names its tab. Without script they are sections one below the other |
| Accordion | parts to open and close, optionally only one open at a time and the first one open |
| Progress bar | how far something is: a name, the value as text and a bar (percent or *value / maximum*); colours primary, success, warning, danger |
| Counter | a large number with a name, optionally with text before and after it (`€`, `%`, `km`); it counts up once when it comes into view (1.2 seconds, never with reduced motion). Screen readers always get the whole number |
| Timeline | entries in order, each with a date or step as its first heading; separate them with a horizontal rule. Top to bottom, or left to right on the desktop (then it scrolls in itself and needs a name; on the phone it goes top to bottom) |
| Dialog (button) | a button that opens the content in a dialog with a title. It never opens by itself; Escape or *Close* closes it and the focus returns to the button. Without script the content opens in place |
| Tooltip | a word in a line with an explanation (`:tooltip[VPN]{tip="Virtual private network"}`): it appears when pointing at the word, with the keyboard focus and when tapping, stays while the pointer is on it and hides with Escape. Screen readers read it as the description of the word; without script it stands in brackets |
| Quote of a person | what someone says, with name, role and optionally a picture from the wiki (a testimonial) |
| Logo bar | logos side by side, for example partners: put the pictures from the media library into it, each with its name as text alternative and optionally as a link; optionally in grey and in colour when pointed at or focused. It needs a name for screen readers (for example *Our partners*) |
| Tag cloud | the tags of an area with its subcategories, or of the whole wiki; the most used larger (or as a list with the number of pages). Each tag leads to the search for its pages. Only pages and categories the reader may see count |
| List of spaces | the spaces (top-level categories) the reader may see, as cards or a list with description and logo (`::spacelist`). Archived and personal spaces only when ticked; at most 200. |
| Details of the space | name, key, description and address of the space the page is in (or of a chosen category, `::spacedetails`); only if the person may see the space. |
| Changes of the page | the latest saved versions of this or another page: date, who (display name) and the note of the change, with a link to the whole history. Only for people who may see the page |
| Contributors | the people who wrote this or another page, the latest first, with the number of their versions; shown by their display name only |
| Person card | a person with picture (from the wiki), name, role, a short text and ways to reach them (e-mail, phone, a link) |
| Profile card | a person of the wiki with photo, job title, department and contact details from their business card (`::profilecard{user=12}`; *Details* with contact details or only name, job title, department). Only people who may find this person see it (everybody with `core.users.list_all`, otherwise the people of the own groups). Guests see no people. |
| People of a group | the members of a group as cards, list or table (`::people{group=3}`), at most as many as the limit `content.team.people` (default 500). Shown only to people who may list the group and its members; guests see no people. For a public page with contact details there is the team page. |
| Decision | a decision with status (open, decided, rejected, postponed), day, who is responsible and a text for the reasons (`:::decision{title="Meetings on Tuesdays" status=decided day=2026-10-06 person=12}` … `:::`). Who is responsible is a person of the wiki (`person`, chosen in the editor), a text such as a board (`owner`), or both. The name of the person is shown like in the people blocks: only to signed-in people who may find this person (themselves, everybody with *list all users*, the members of their own groups with *list users of the own groups*); guests and others see only the text. Disabled users are not shown. Its data is kept for the report of decisions when the page is saved. |
| Report of tasks | the tasks of pages (task items with `@name` or a date) of an area as a table with *Done*, task, who is responsible, due date and page (`::tasks{category=3 due=overdue}`). *Whose tasks*: all or *Mine* (`who=mine`; guests are asked to sign in), *Status*: open (default), done (`status=completed`) or all, *Due*: all, overdue, within 7 days or overdue, without a date; ordered by due date or by page, at most 500. Overdue tasks are marked with the word *Overdue*. A checkbox appears where you may tick the task; otherwise the status is shown. Names as in the people blocks. |
| Report of decisions | the decisions of an area (or of the whole wiki) as a table with status, day, who is responsible and the page (`::decisions{category=3 status=open}`), newest or oldest first, at most 500. *Whose decisions: Mine* (`who=mine`) lists only the decisions the reader is the responsible person of, for example on a personal start page; guests are asked to sign in. Only decisions on pages the reader may see. While the wiki builds the data (after an update), a note says that some may still be missing. |
| Heading with changing words | a heading in which one word changes (`::rotating{before="We build" words="wikis|sites|tools"}`), faded or typed. Screen readers get the whole heading with all words; a button pauses it; it stops while pointed at or focused and stands still with reduced motion |
| Ticker | the entries of a list run through a band; always with a pause button, it stops while pointed at or focused, stands still with reduced motion, and the list stays readable for screen readers (once) |
| Content slider | parts one at a time (separate them with a horizontal rule), with *Previous*, *Next* and a button per part; optionally it changes by itself every 7 seconds, with a pause button, never with reduced motion. Without script the parts stand one below the other |
| Price table | up to 4 plans side by side (separate them with a horizontal rule): per plan a heading as its name, the price, a list and a button. *Highlighted plan* marks one with a label (default *Recommended*, never by colour alone). Plans move below each other where they have less than 14 rem |
| Picture with hotspots | numbered points on a picture of the wiki; each entry of the list is `x/y: text` (x and y in percent from the top left corner, e.g. `30/40: Entrance`), at most 20. The texts always stand numbered below the picture; a point is a link to its text (keyboard and screen readers: *Point 1* …). The description of the picture is required |
| Before and after | two pictures of the wiki of the same size over each other with a slider between them (mouse, touch, arrow keys, Home, End); both need a description, the captions default to *Before* and *After*. Without script both stand side by side |
| Countdown | the time left until a day and time in the time zone of the wiki, in days, hours and minutes; new each minute, without announcements, with a pause button. The date always stands below it; once it is reached, the *text once reached* appears |
| Newsletter sign-up (external) | a form for the newsletter of an external provider: the reader enters the e-mail address (and, if the provider asks for it, the name); the browser sends it straight to the provider, the wiki stores nothing. The form says to which provider the address goes and links its privacy policy. The address of the sign-up, the names of the fields and fixed values (such as the number of the list) come from the provider (`::newsletter{address="https://news.example.com/subscribe" hidden="list=7"}`). Only providers the administrators allowed work (*Administration → Settings → Media*); otherwise the block names the provider and how to allow it. The own newsletter of the wiki, with sign-up in the wiki, will be a plugin of its own |
| Map | a place on a map: coordinates (`lat`, `lon`, `zoom` 1–19) show OpenStreetMap with a marker, or *address* shows the map page of another allowed service (for example a map server of your own). The place in words (*place*: address, building, how to find it) is required and always stands below the map. Like an external video, the map loads only after the reader agrees, and only from services the administrators allowed (*Administration → Settings → Media*, e.g. `https://www.openstreetmap.org`). A map with markers of its own and keyboard use is planned for later. Example: `::map{place="Main entrance, Example Street 1" lat=46.6167 lon=14.2653 zoom=17}` |
| Chart | bars, lines or a pie from a table in the block: the first row names the series, the first column the categories, the other cells are numbers (written as usual, e.g. *1.234,5* in German, *1,234.5* in English; a unit such as € in the cell is ignored). The server draws the chart; screen readers get its title and the table, which stays below the chart (*Data as a table*, folded or open). At most 6 series and 50 rows; a pie shows one series with at most 12 slices. Lines differ by the shape of their points too, not only by colour |
| External video or embed | a video or page of a service outside the wiki, for example a link to a video (address with https). Only services the administrators allowed work; otherwise the block names the service and how to allow it. Readers first see a box with the name of the service and load the video only with *Load content*; only then does the service receive data such as the IP address. With *Always load content from …* the browser remembers the consent; the button below the video takes it back. The title is required: screen readers announce it for the video. A link opens the video on the service itself |
| Sign-in box | for pages with a members' area. Guests see *Log in* (back to the same page afterwards) and, if chosen, one button per sign-in service the administrators set up. Signed-in people see their name, optionally *My dashboard*, and *Log out*. The block never takes a password itself: signing in always goes through the sign-in page of the wiki with all its protection. Title and texts for guests and signed-in people are free; to address only guests or only signed-in people, also use the display options |
| Icon | an icon of the shipped set (more than 6,000, outline or filled), also inside a line (`:icon[Phone]{name=phone}`); chosen with a search (in German also with German words such as *Pfeil* or *Uhr*; the categories and the names of the icons are German then). Screen readers read its label or, without one, the name of the icon in the language of the page (for example "arrow right", on a German page "Pfeil rechts"; words without a German term stay English); only an icon marked *decoration only* is skipped |
| Icon box | an icon with title and text; with a link the whole box is one link. The icon is read like a single icon |
| Icon list | a list with an icon in front of every entry, for example a check mark. The icon takes the place of the bullet: screen readers read the list as a list, without the icon |
| Include page | shows another page (or only its excerpt) here, always as it is now. Everybody sees only what they may see; a page classified stricter than this one is not shown here (not even its title), so it does not travel on with this page by e-mail, print or export. A page includes at most `core.content.includes_per_page` pages, counted over all levels (expert setting) |
| Excerpt | marks a part of this page that *Include page* with *Only the excerpt* shows; with names there can be several excerpts on a page |
| Anchor | a point in the text that a link with `#name` jumps to (`:anchor{name=budget}`). Names: lowercase letters, digits and hyphens, without `ew` at the start and without `-number` at the end (the wiki names its own fields like that; likewise not `f` with a number and a hyphen at the start); every name once per page, and not like a heading. Headings get their jump address from their text (`#budget`); if it would be an address of the wiki (for example `main`), it starts with `h-` (`#h-main`). Every address exists only once on a page, also with included pages (there with a number, for example `#intro-2`) |
| Date | a real date in the text, written for the language of the page (`:date{value=2026-11-22}` becomes “22 November 2026”); long, short (2026-11-22) or with the weekday. For programs and screen readers the date is always also machine-readable (`<time datetime>`). With a **title** it is an **appointment** (`:date{value=2026-12-01 label="Team meeting" time=10:00 end=11:30}` becomes “1 December 2026, 10:00–11:30”): it appears in the *Report of dates* and in calendars that take the page or its category as a source. Start and end are on the same day, in the time zone of the wiki. |
| Report of dates | the appointments in pages of an area (or of the whole wiki) as a table with day, time, title and page (`::dates{category=3 range=upcoming days=30}`): upcoming (from today), past or all, optionally only so many days ahead or back, at most 500. Only pages the reader may see. |
| Formula | a mathematical formula in TeX, for example `:math{tex="a^2 + b^2 = c^2"}`; *Shown* in the line of text or on its own line. A code block with the language *Formula (TeX)* (```` ```math ````) is set as a formula too. The browser shows it as MathML, and screen readers read it as mathematics. Without script and for TeX that is not valid, the TeX text stays readable. Colours (`\color`) are not taken over. |
| Page tree | subcategories and pages of a category as a tree to open, with a field to filter it; up to four levels |
| Search box | a search field for this (or a chosen) category with its subcategories, or for the whole wiki; the search page shows the results, and there the search can be widened to the whole wiki. While typing, the first results appear below it (can be switched off); Escape closes the list |
| New page from a template | a button that opens a new page in a category, filled from a page template. Only people who may create pages there and can use the template see it; so take a template for everybody or for the space |
| Attachments | the files this page shows, or the files of a category (optionally with subcategories), as a table with kind, size and date, sorted by name, date, size or kind. Everybody sees only files they may see; files that may only be viewed have no download. People who may create or edit pages in the category upload a file right there with *Upload a file* |
| Shell command | output of a command on the server; only runs once a super administrator has approved exactly this command |
| HTTP request | result of a web address (text, HTML, JSON as a table); only approved addresses |
| News from several sources | the newest elements of any number of sources and entries of external RSS/Atom feeds together, as cards, list, headlines or tiles (pictures in at most as many columns as chosen per view, each at least 12 rem wide, optionally as masonry); with *Filter by tag* buttons above the list show only entries with one tag |
| Blog posts from several sources | posts of several sources in the look of the blog (list, cards, compact) |
| FAQ from several sources | questions of several sources as one FAQ, one expandable section per category |
| Recently changed, upcoming events | also with freely selectable sources |

You put together **sources** in blocks, blog, FAQ, calendar, carousel and property reports in the same way with the **source builder**: on the left are the building blocks, a click (or Enter) adds one under **Elements from**, and with the mouse they can also be dragged into an area. **Elements from** – one source is enough; **Exclude** – elements matched by one of them never appear (e.g. tag "internal"); **Must also match** – only elements additionally matched by each of them. Every entry can be moved into another area through its selection field (or by dragging the handle ⠿). Below, the preview shows how many elements result with your permissions. Building blocks are a **category** (with or without sub-categories), a **tag**, an **element type**, **single elements**, a **search** (all hits for search terms), a **property filter** (e.g. `status = open; due <= today+7d` – all elements whose properties match), depending on the place also an **external feed** (RSS/Atom), **another calendar** or an **iCal subscription**. In the Markdown source, sources are separated by semicolons, e.g. `::news{source="category:12+sub; tag:hr; feed:https://example.org/rss"}`; the filters of a property source are separated by `&&` there (`props:status = open && due <= today+7d`). Exclude is written as `-`, "must also match" as `+` before the source (`category:12+sub; -tag:internal; +props:status = open`). Without a source – or only with exclusions – the current category with sub-categories applies.

**Maximum and pages:** for lists from sources (news, blog, FAQ) you choose the **maximum number of entries** and whether the list is **split into pages** – with how many **entries per page**. Pages change without reloading, the focus jumps to the start of the list; every page has its own link. The administration sets the upper limit (default 1000).

In blocks everyone only sees what they may see anyway.

### Processes (workflows)
With *New page*, choose the element type **Workflow**. With *Template* you take over a finished process (e.g. *HR: application*) and adapt it:

- **Start form:** fields that are filled in at the start (text, e-mail, date, choice, file, confirmation …).
- **Steps:** every step has a kind:

| Kind | Effect |
|---|---|
| Task | those responsible decide with one of the **actions** (e.g. *Invite to interview*, *Reject*); every action leads to a next step and can require a note |
| Approval | four-eyes principle: a certain number of approvals or all of those responsible (every member of their groups and roles, counted together) must approve; a rejection (with reason) ends the approval |
| Condition | checks data from the form automatically (e.g. "amount greater than 1000", "position is software development") and branches |
| Send e-mail | sends a prepared e-mail automatically (recipients, Cc, sender, reply address, subject, text) |
| Webhook | hands the case over to another system (e.g. HR software) |
| Waiting time | waits days/hours or until a date from the form (e.g. the start date) |
| Message | sends a message in the wiki (bell) and/or by e-mail to `starter`, `actor`, `assignees`, `user:NAME`, `group:NAME`, `role:NAME` or addresses; with *Show as a pop-up too* the message in the wiki appears once as a dialog when the next page loads |
| Set values | sets values of the case: text with placeholders or a formula (`round(quantity * price, 2)`, `if(amount > 1000, 1, 0)` …); later steps use them as `{{field.NAME}}` and in conditions |
| HTTP request | queries another system (only addresses of the allow list `plugins.macros.http_allow`) and takes over values of the JSON answer (path such as `data.status` or `entries.0.id`) |
| Create page | creates a page (Markdown with placeholders) in a category and remembers ID and address for later steps |
| Set properties | sets properties of a page (e.g. of the one just created: `{{field.page}}`) |
| Permissions | grants a person (user name, alias, e-mail or ID, usually a placeholder such as `{{trigger.data.username}}`) or group read, edit or manage permissions on a category – if desired only for **N days**, after which they are withdrawn automatically – or withdraws them |
| Group membership | adds a person to a group or removes them from it |
| Move page | moves a page into another category, e.g. into the archive |
| Create file | writes a document from a template into the media library: PDF, Word (.docx), Excel (.xlsx), CSV, JSON, text or Markdown – lines with `#`, `##`, `###` become headings, if desired with all fields of the case (Excel and CSV always as a table). ID and address of the file end up in fields (`{{field.file}}`) |
| Upload or confirm a file | a task with one purpose. *Upload*: the assignees add a file to a file field of the case (for example a signed contract); optionally it also goes into a category of the media library, with the rights of the person who saved the workflow. *Confirm*: the assignees see a file of the case and confirm or reject it; a rejection needs a reason and leads to the step after a rejection. Confirming can require a signature |
| Shell command | runs a command on the server (only if the server allows shell commands). A superadmin must approve the exact command first; the case waits until then. Values of the case reach the command only as environment variables of the chosen fields (`EWIKI_FIELD_NAME`), never as text of the command. Output and exit code go into fields; with an exit code other than 0 the case goes to the step *otherwise*. Test runs do not run the command |
| Calendar entry | creates an event in a calendar: title, start (date, date with time, placeholder such as `{{field.date}}` or relative `+3d`, `+2h`), duration, all day, location, time zone, registration. A following message step sends invitations with the stored address |
| Further workflow | starts a case of another workflow with values; if desired waits until it has ended, then takes over values and branches on failure |
| Parallel branches | starts several branches at the same time – fixed steps or **one branch per value** of a field (e.g. one review per department, the value is `{{field.item}}`); every branch ends at the **meeting point** (next step). Optionally *k* of *n* branches are enough, the others are then ended; values of the branches are taken over |
| End | closes the case with a result (successful, not successful, neutral) |

Automatic steps with permissions (HTTP request, create page, properties, create file, calendar entry, permissions, group membership, move page, further workflow) act with the permissions of the person who saved the workflow (or of the starting person) – nobody gets more through a workflow than this person may do themselves.

For tasks and approvals a **reminder after X days** can be set. Those responsible receive an e-mail for new tasks (can be switched off in the profile).

**Deadlines:** tasks and approvals can have a deadline – days/hours from reaching the step or from a date of the form, if desired only **working days** (without weekends and the public holidays under *Administration → Workflow*). Before it expires, those responsible are warned (e.g. 48 and 4 hours before). If the deadline expires, the chosen **escalation** happens once: only a message, hand over to someone else, decide automatically (an action or approval/rejection) or cancel the case – each optionally with a message.

**Not before:** a date field can follow another date field (last day not before first day). The start form, the forms of steps and the server refuse a date before the other one. **Form of a step:** fields with *Filled in later* do not appear in the start form. For tasks and approvals, *Form of this step* sets which fields those responsible fill in (optional or required), only see or **do not see** (e.g. the salary). Whoever started the case, managers and admins always see everything.

**Those responsible** for a task, an approval or an upload: the person who started the case, people, groups and roles, several of them together (at most 20 entries, for example one person, two roles and three groups); any one of them may act. In the editor you choose them under *Handled by*: people by name (from 2 letters), groups and roles from a list. A group or role is kept by its number, so renaming it changes nothing; a name from a workflow template that this wiki does not know stays listed with a warning until you choose again or remove it.

**Handing over tasks:** for tasks of a group, a role or several people, a person can **take over** the task (the others then no longer see it) and **give it back**. Every responsible person can **pass it on** (to someone who may see the workflow) or ask the **applicant a question**: they answer with a note, then the task comes back. Everything is in the history.

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

On the page, *Start new case* starts a case; a workflow can name this button itself (*Label of the start button*, e.g. “New holiday request”). The table shows the cases you may see; "Your turn" marks where you can act. A click on the title opens data, history and actions. **My tasks** (sidebar or `/tasks`) collects everything waiting for you. Running cases keep the process they were started with, even if the page is changed later.

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
- **Settings per field:** label, ID (for placeholders and exports), help text, required field, example in the field, default value, prefill (name, e-mail or user name of the signed-in person, value from the address `?name=`), minimum/maximum length, pattern with explanation, minimum/maximum/step, autocomplete (WCAG 1.3.5), **Not before** a date, time or date-and-time field above (e.g. the last day not before the first day; the date picker then offers no earlier day, and the form refuses a wrong pair) and **Only show if** a field above has a value (is, is not, is empty, is filled, contains). Hidden fields are neither checked nor sent.
- **File upload:** allowed kinds (images, PDF, office, text/CSV, ZIP, audio, video) or extensions, number and size per file, comment per file, optionally additionally into a category of the media library. Senders choose files or drag them onto the upload area. Every file is checked by its **content** (does it match the extension?), office files with **macros** and PDF files with **scripts, actions or embedded files** are rejected (can be allowed per field), a configured **virus scanner** checks additionally.
- **Layout:** *Grid* (recommended) – every field takes 1 to 12 columns, on narrow screens below each other. *Freely placed* – fields are dragged on an area and resized at the corner (keyboard: arrow keys move, with Shift the size, with Alt in 1-pixel steps; plus number fields). Screen readers and keyboard always follow the order of the field list; on small screens the grid applies.
- **Submitting:** only signed-in people, once per person, period (opens/closes), maximum number, delete after days, copy to the sender, **arithmetic task against bots** (the browser computes for about a second, no CAPTCHA), **results public** (frequency of the answers for everyone who sees the form).
- **What happens with a submission:** keep in the wiki, **e-mail** to addresses, `user:NAME`, `group:NAME` or `field:ID` (an e-mail field) with subject and text including placeholders, **webhook** (JSON, signed), **entry in a property database** (fields → properties), **start workflow** (fields → fields of the workflow), **store as file** (PDF, JSON, CSV or text in a category of the media library, file name with placeholders). External e-mails and webhooks need the permission *Send submissions outside*. These targets are executed with the permissions of the person who created the form – if they lack a permission in the meantime, the target is not executed and shown as an error with the submission. No content leaves confidential areas by e-mail or webhook, only the link.

**Every person who may see the form may submit it** (also guests if the category is public). Errors are at the field and collected at the top with links to the fields; the focus jumps there. A hidden field, a minimum time and limits per address or account help against automatic submissions – without CAPTCHA.

**Submissions** are listed below the form, with search and files for download. They have their own permissions: *See submissions*, *Export submissions* (CSV or Excel; values starting with `=` stay text), *Delete submissions* and *Manage submissions* (all three). Exporting and deleting include seeing. The person who created the form always sees and exports its submissions; to delete, they need the permission as well. Deleted submissions cannot be restored. *Forms administrator* includes *Manage submissions*. A link in the e-mail leads directly to the submission. The block **Form results** (`::form-results{form=ID}`) shows the frequency of the answers as a table with bars (with public results for everyone, otherwise only with *See submissions*) or with `show=table` the submissions as a table (only with *See submissions*), with maximum and pages.

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
  - **Further sources**: events of other calendars (only those the readers may see; they are changed in their calendar) and **iCal subscriptions** of external calendars (e.g. public holidays; updated every 10 minutes, read-only, the description as plain text). Every event shows which calendar it comes from. **Pages as a source**: a category (with or without its subcategories) or single pages; their appointments in the text (date blocks with a title) appear read-only with a link to the page, only for readers who may see the page; with *Also show the open tasks with a due date of these pages* their tasks too. The block *Upcoming events* takes the same sources.
  - The block **Upcoming events** shows coming events on every page – from this one, from several calendars or from iCal subscriptions.
- **Team**: name and description (Markdown/HTML), one or more groups whose members are shown, and the selection of the data: photo, function, department, e-mail, phone, mobile, office, "About me", groups or attributes from LDAP/OIDC/SAML. Shown as cards, list or table, if desired structured by groups, with filter and CSV export. Every person maintains the data under *My profile → Business card*, administrators under *Administration → Users*. Guests see only name, job title, department and photo; with *Show the contact details to guests too* (only with the permission *Show contact details publicly*) also e-mail, phone and the other details. Attributes of the identity provider are chosen only by who manages users.

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
- **Block "Property report":** in the dialog of the block choose sources (categories with/without sub-categories, tags, element types, single elements), filters with matching conditions per type (e.g. *Due date at most today+30d*, *Responsible is me*), columns, sorting and display: **table** (with groups, subtotals and totals), **list**, **cards**, **board** (move cards into another column by selection field or dragging), **calendar**, **timeline**, **chart** (columns, bars, stacked, line, area, pie, donut, heatmap), **key figures**, **pivot table**, **tag cloud**, **completeness** (which values are missing), **overdue and due soon** and **locations**. Readers can set their own filters, download as CSV or Excel and **subscribe** to the report (daily or weekly by e-mail, always with their own current permissions; confidential entries only with title and link). The display **Trend** shows from the change history how a choice, status, yes/no or number property has developed week by week (e.g. open and completed tasks, sum of hours). Strictly confidential entries are never contained in downloads and e-mails. Charts have a text summary and the data as a table.
- Everything only shows what you may see: elements without read permission and properties with restricted visibility are also missing in filters, totals and charts.

### Addresses and "Page not found"
Every page and category has **a readable address per language** from its title, e.g. `/wiki/unser-team/ueber-mich` and `/wiki/our-team/about-me`; without a translation the address of the original language applies. All language addresses work, the address bar shows the address in your language. With **Only this short URL** (at the short URL in the properties) a page can only be reached through its short URL. If a page is renamed or moved or a short URL changed, the **old addresses still lead to the target** (permanent redirect). If an address leads nowhere, the wiki shows whether the page was deleted (with a link to the trash for everyone who may restore) and suggests matching pages ("Did you mean?") – only those you may see.

### Short URLs

Elements and categories can get a **short URL**: pages through *Properties* (there also the **preview image** that appears when sharing a link in Teams, Slack or Outlook – without one, the first image of the page), categories through *Edit category* or the settings. With `calendar` the element can then be reached under `https://your-wiki/calendar`. Sub-levels such as `team/hr` are possible. Every short URL exists only once; words the wiki uses itself (`admin`, `login`, `search`, …) are blocked. Menus and overviews link the short URL automatically.

### Start page and category overview

Under *Administration → Settings → Start page*, administrators choose what appears under `/`: the overview of the top categories, a category or an element. New installations show the welcome page. The element type **Category overview** is suitable for an overview of areas: one category or the top level, with 1–5 levels of sub-categories, as cards or list, if desired with descriptions and the pages of every category.

### Display of a category

Under *Category settings → Appearance and content → Display of the pages* you choose how the category page shows its pages (*Manage* needed). The choice also applies to the subcategories until one of them chooses its own; *As above* takes it from above again.

- **Kind:** *List* (title, beginning of the text, date), *Cards* (with the preview picture of the page), *Tiles* (large pictures), *Tree* (subcategories to open with their pages) *Handbook* (the pages as a navigation in the side column, also on every page of the category; the page shown is marked; at the end of every page *Previous* and *Next* lead to the page before and after, on the page of the category *Next* to the next one), *Table* (sortable by every column with a click or the keyboard; further columns such as properties are chosen below, everybody sees only values they may see), *Accordion* and *Tabs* (the pages open in place; in the tabs the arrow keys, Home and End move between the pages), *A–Z* (by the first letter), *Timeline* (by the date of the change or the creation, under the month) or *All one below the other* (the pages in full, for short pages). Accordion, tabs and all one below the other show at most `core.categories.full` pages at once.
- **Order:** as ordered in the category, by title, last changed first or last created first.
- **Pages per page** (more are paged), **also the pages of the subcategories** (without hidden and archived ones) and **show the beginning of the text**.

**Arrange category page** (edit mode, or in the same settings tab) arranges this one category page, not its subcategories: title, buttons, description, overview and subcategories are blocks at the positions *header*, *toolbar*, *content*, *side column* and *below the content*. Here you also choose **featured pages**: pages shown in full, each at a position, e.g. a welcome page in the content and a contact page in the side column. The default page of the category is the first featured page. Every reader sees a featured page only if they may view it.

### Areas
Every top-level category is an **area**. Whoever has the permission *Manage* there is area management – without any administrator permissions. Under *Category settings → Area* they set what makes an area:

- **Area key** (e.g. `HR`): the area can then be reached under the short address `/s/HR`; keys are unique.
- **Offer as template for new areas** (only with the permission of category administration), export and import.

Under *Category settings → Appearance and content*, **every category** (with *Manage*) sets what applies to it and its subcategories, as long as nothing of their own is set below; a note shows from which category a value is inherited:

- **Own navigation**: from here the sidebar only shows the categories below, with name, logo and shortcuts at the top.
- **Shortcuts**: important pages or external addresses at the top of this navigation.
- **Allowed element types**: only these types can be created here or moved here; *New element* only offers them. Below, the choice can only be narrowed further.
- **Language of new elements**: default language for new pages.
- **Allowed blocks**: the editor only offers these blocks here; others are not executed, but shown as a notice. Below, the choice can only be narrowed further.
- **Logo** and **accent colour**: logo in the navigation and in the directory; the accent colour applies to links and buttons and must have a contrast of at least 4.5:1 with white.

If a category is moved to the top level, it becomes an area; if an area is moved below another category, it is no longer one (the key becomes free). The settings under *Appearance and content* stay in both cases.

*All areas* (`/spaces`, link at the bottom of the navigation) lists all areas you may see, with a filter. Name and shortcuts of an area are only seen by whoever may see its top category.

**Page templates of an area or a category:** with *Save as template*, whoever manages the category chooses "Everyone in this category and below" (area management also "Everyone in the area …"). Such templates are only offered when creating pages there and below.

**Archiving** (*Category settings → General → Archive*): the category with everything below becomes read-only for everyone – also for administrators –, disappears from navigation, overviews and search and shows a notice. Direct links keep working; archived areas are listed under *All areas* in their own section. *Restore* makes everything editable again.

**Area templates:** category administrators can *offer an area as a template for new areas*. *All areas → New area* then creates an area with sub-categories, permissions, settings, website template and page templates of the template – if desired also with the pages.

**Permissions of an area** are the completely normal permissions of its top category (*Category settings → Permissions*) and therefore also appear in *Administration → Permissions* – areas are marked there. Whoever has permissions as a person, through a group or a role is a **member**; *All areas → Only my areas* shows these.

**Moving:** every category can be moved at any time. If it becomes top level, it is an area (earlier area settings apply again, the key must be assigned again). If an area is moved below another category, it releases its key; its settings and page templates rest, and those of the new area apply. Moving and copying into an area is only possible if its allowed element types match.

**Requesting access:** whoever may not see a page or category gets "Not found" (whether it exists is not revealed) and, when signed in, can *Request access*. The people who manage the category receive a notification and answer open requests under *Category settings → Permissions* with *Allow reading*, *Allow editing* or *Reject*; this becomes a normal permission rule. For a page with *own rights only*, the request goes to everybody who may give rights on that page; they answer it in the *Rights* of the page, and it becomes a rule on the page. At most 10 requests per person and day.

**Personal area:** with the permission *Create personal area*, every person creates their own area under *All areas*, which only they see until they share it through the permissions.

**Classification** (*Category settings → General → Classification*): *Public*, *Internal*, *Confidential* or *Strictly confidential*; it applies to everything below, and the strictest classification above always applies (a sub-category can only become stricter). Consequences:

| Classification | Effect |
|---|---|
| Internal | never for guests – even if a rule allows guests to read; notice "Internal" on every page |
| Confidential | additionally not in Atom/RSS feeds and calendar subscriptions, watch e-mails only with title and link, notice on every page; images and PDFs of the media library as well as images in pages carry the name of the viewer and the time as a watermark |
| Strictly confidential | additionally not in the general search (only when searching within the category), not in external search indexes, no export, no copies; files of the media library and file elements can only be viewed, not downloaded (except with manage permission) |

**Protection of files selectable:** in the *Category settings*, below the classification, there are *Watermark for files* (*Automatic* or *Off: always the original*) and *Downloading files* (*Automatic* or *Allowed: download the original*). Without a choice the setting above applies; a sub-category can switch the protection on again. With *Duplicate*, every copied category keeps its classification, watermark and download setting; the copy is never protected less than the original.

**Settings of a file take precedence:** what is set directly on a file of the media library – own permissions, *View only* or a watermark – cannot be lifted by settings of the category or the area. Category and area can only restrict additionally. Such files are also not delivered without these restrictions through a page that embeds them.

**Downloads of reports and databases:** selectable when creating or editing: *Safe* (default: values with `=` at the start stay text, so Excel does not execute anything unexpected) or *Original* (such values become formulas – only for trustworthy content).

**Evaluation** (*Category settings → Evaluation*, with *Manage*): views per day, most-read pages, pages not changed for over six months and contributors of the last 7 to 365 days. Views are counted sparingly only per page and day – without people and addresses – and deleted after 400 days.

**Summary of an area:** *Watch* on the top category with sub-categories and a daily or weekly summary sends all changes of the area collected by e-mail.

**Exporting and importing** (*Category settings → Area*): the export is a file (JSON Lines) with all sub-categories, pages (last version of every language), settings and page templates. *All areas → Import area* creates a new area from it, *Import here* a sub-category. Permissions and files are not contained (they depend on the installation); all content is checked and sanitised on import as on saving, entries that cannot be taken over are listed.

### Own website template per category
In the *Category settings*, whoever may manage the category chooses one of the active templates under **Website template** (e.g. *CMS* for a public area within an internal wiki). It applies to the category, its sub-categories and all pages in them; a sub-category can choose its own again (the nearest wins). *Inherit* uses the template above or that of the site. When changing between areas, the template changes without reloading; content, focus and permissions stay the same.

### Carousels
Carousels show at a position of the template (e.g. above the content of every page, that is below the header and above path and title) news, own slides, entries of external feeds and the newest elements of any **sources** (categories, tags, element types – with title, summary and preview image; every person only sees what they may read). With **Publish with options**, with the permission *Publish news in carousels*, you choose "As news in …" and optionally an end date; news is only seen by people who may see the page. With *‹* and *›* or the dots you change the slide; if the carousel runs automatically, *Pause* stops it (it also stops as long as mouse or focus are in it, and does not run at all if your system requests reduced motion). A carousel with *No fixed position* appears only where it is placed under *Administration → Placement* or in the placement of a category, for example only in one category. An own slide has title, text and alternative text in the default language and, below them, in every other language of the wiki; readers see the slide in their language, and the default language where a translation is empty.

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

The **bell** next to your name shows notifications in the wiki, the number of unread ones is next to it. A click on a notification opens the target and marks it as read; *Mark all as read* empties the list of unread ones. The ✕ next to a notification deletes it.

**All notifications** (link at the bottom of the bell, page `/notifications`): all your notifications, newest first. *Show older ones* loads the next ones. *Unread* shows only the unread ones. Each entry shows who caused it; a click on the name opens the menu on names, if you may open that person. *Mark as read* and *Delete* act on one entry, *Mark all as read* on all.

- **Share** (button on every page) points people, groups or roles to the page with an optional message. Sharing gives **no permissions**: whoever may not see the page is not notified – *Check recipients* shows this beforehand.
- **Choosing people, groups and roles** works the same everywhere (sharing, permissions, tasks, properties): short lists are checkboxes. Longer lists have a filter field above them: type to narrow the list, arrow down goes into the list. There the arrow keys move, Space (or Ctrl+Space) chooses or removes an entry, Shift+arrow chooses a range, Ctrl+A all shown entries; typing a letter goes back to the filter. People appear after 2 letters of their name. What you chose stands above the list, each with a button to remove it, also when the filter hides it. The administration sets from which length a list gets a filter.
- **@mentions:** type `@` and the name in a page or a comment; the mentioned person receives a notification if they may see the page. Readers see the display name of the person, with the menu on names, if they may find the person. All others see only `@na****` (or, if the administration chose so, the display name without the menu).

Some notifications are **pop-ups** (for example the result of a request, if the workflow says so): they appear once as a dialog when the next page loads. *OK* or Escape closes it, *Open* leads to the target. Afterwards the message stays, read, in the bell. Several pop-ups come one after the other, at most `core.notify.popups` per page load (expert setting).

In addition an e-mail comes each time, as long as you do not switch it off under *My profile → E-mail notifications*.

## Feeds (Atom/RSS)

In the *Category settings* an **Atom/RSS feed** can be switched on (only this category or with sub-categories). Blogs and FAQs have their own feeds. The button **Feed** shows the addresses for feed readers. Signed-in people get a **personal link** with a key, so the feed reader can also read protected content. Under *My profile → Feed and calendar links* you renew or delete the key if a link was passed on.

## E-mail notifications
Under *My profile → E-mail notifications* you set what you are informed about by e-mail: new tasks and reminders, completion of your cases, comments on your pages and answers in discussions as well as changes to watched content. This requires an e-mail address in the profile. If sending e-mail is switched off by operations, the profile points this out.

## Comments
Below every page, signed-in users can comment and answer comments. Your own comments can be edited and deleted. Links are linked automatically.

## History and restoring

Every save creates a new version. Nothing is ever overwritten. Under **History** you can view every version and **restore** it. This creates a new version with the old content.

You reset a whole category to a **point in time** under *Category settings → History & reset*, optionally with all sub-categories. Titles, texts and pages get the state of that time, pages created later go into the trash. With **Include moved and new categories** (only together with all sub-categories) the structure gets the state of that time too: categories and pages moved since then go back to their former place and order, also into or out of the category; categories created since then (also copies) go into the trash with their pages. The category itself keeps its place. If a former place no longer exists, nothing is changed and the message names it; restore it from the trash first. This too can be undone again.

## Trash

Deleted categories (with all content) and pages end up in the **trash** (user menu). From there you restore them. They are only deleted permanently by administrators or automatically after the set retention time.

## Search and tags

- The **search** finds words and word beginnings in titles, tags and content of all languages. Hits are highlighted.
- **Search in a category:** a search box on a page (block *Search box*) searches only in its category and the subcategories. The search page names the category; without the tick at *Only in …* it searches the whole wiki.
- You assign **tags** to categories (settings) and pages (*Tags*). A click on a tag shows all content with this tag. On the search page there is a list of popular tags.

**Hidden** (under *Properties* of a page) takes it out of lists, blogs, FAQs, blocks, feeds and the search; it stays reachable by its link for everybody who may read it, and editors see it marked in the category. Hidden is not a protection – use permissions for that.

**Rights of a page** (button *Rights* in edit mode, for everybody who may give rights on it): rules for people, groups, roles, all signed-in people or guests apply in addition to the permissions of the category – for example, one person may edit exactly this page. Possible rights: *View*, *Edit*, *Delete* and *Give rights*; nobody gives more than they have. With **Own rights only**, the permissions of the category no longer apply to this page: only people with a rule see it, as well as administrators and people who manage the category through a rule. For everybody else such a page appears nowhere – not in lists, search, blog, feeds, blocks, notifications or exports. The **classification** of a page can be stricter than that of its category (the strictest applies). Nobody can take the right *Give rights* away from themselves. When a page with rights of its own is copied, the rights are copied by default; only people who may give rights may copy it without them. Moving keeps the rights, so does the trash.

## My dashboard

Under *User menu → My dashboard* (`/dashboard`) you see tiles at a glance: notifications, your tasks, the cases you started, pages you edited last, favourites, new submissions of forms whose submissions you see, upcoming events, recently changed pages and watched pages. Administrators also see the *State of the wiki*: version and updates, e-mail and queue, failed sign-ins of the last 24 hours, forwarding of the audit log, plugins, license and database; a problem is marked with *Attention* or *Error*. Plugins can bring more tiles. Every tile shows only what you may see. Administrators see more in the *Overview* of the administration (`/admin`): state, updates with the commands to install them, security, plugins, users and license.

- **With the mouse:** switch on *Move with the mouse* and drag a tile to another place. The arrangement is saved at once.
- **With the keyboard:** *Arrange tiles* opens a list. In it you choose the position of every tile (*full width* or *column 1–3*) or *Not shown*, and change the order with ↑ ↓. A tile is wide when it is at the position *full width*.
- You always arrange your dashboard yourself, also when the personal arrangement of the rest of the interface is switched off. The administration sets the default and required tiles, which you cannot move, and chooses which tiles are offered. On small screens the columns are shown one below the other.
- If the administration makes the dashboard the start page, you land on it after signing in and at `/`. The start page of the wiki stays at `/start` (link *Go to the start page of the wiki* on the dashboard).

## Placement

Where search, language, edit mode, notifications, user menu, category tree and the building blocks of plugins appear is set by the administration under *Administration → Placement*: per block the position of the template, the order (buttons ↑ ↓) or *Not shown*; *Required* prevents changes further down. Whoever manages a category changes this under *Category settings → Placement* for it, all pages in it and its subcategories; what is not changed there is inherited. Each block placed there can be limited: without *On all pages of the category* it appears only on the page of the category itself (for example a carousel only on the start page of a category), without *Include subcategories* not in the subcategories (there the placement from above applies). The same goes for a block the category hides: the list *Hidden in this category* has both boxes (for example hidden on the page of the category, but shown on the pages in it) and *Show as above again*. If the administration allows it, every person arranges the blocks for themselves under *My profile → My arrangement*. The user menu always stays reachable. Every menu, carousel and content block is a block of its own: the position chosen when creating it is its default; in the placement it can be moved, hidden or shown in several places for the site, a category or (if allowed) for oneself. The list of favourites is a block too (default: sidebar). On a page, the content itself is a block at the position *Page: content*, so other blocks can go above or below it in the main area.

The block *Reading progress* is not placed by default: placed for example under the header, a thin bar fills while you read the content of a page. It only shows where you are; screen readers skip it.

**Light and dark:** under *My profile → Colour scheme* you choose *Same as system* (follows the setting of your device), *Light* or *Dark*; *Default of the template* lets the template decide. The block *Light and dark* offers the same choice with three buttons (*Auto*, *Light*, *Dark*; the pressed one applies now). It is not placed by default. Signed in, your choice applies on all devices; as a guest, the browser keeps it in a small cookie. Templates with light colours only (templates of the designer without dark colours) stay light; the block says so.

**Shared block (synchronised):** select text and blocks in the editor and choose *Save the selection as a shared block* in *Insert block*. The selection becomes an element of the type *Block* in the category and is included in its place (`::include{page=ID title=false}`). On other pages you include the same block with *Include page*. Change the block on its own page, and the change shows everywhere. Each person sees it only if they may see it.

**Your own blocks (type *Block*):** a text that should appear at a position (such as a contact in the side column or a footer) is a new element of the type *Block* in a category, written like a page. A block is never in the search and not in lists of pages. On its page, *Where this block is shown* lists all placements; *Place the block* chooses the position and whether it appears in the category (with or without subcategories, optionally only on its page) or – with the permission for the settings – on the whole site. Each person sees it only if they may see it. Once a block appears on the whole site, only people with the permission *Edit blocks of the whole site* change, move or delete it.

**Options of a block:** next to each placed block, *Options* sets how it looks and when it is shown at this position, in groups:
- **Showing:** hidden on desktop, tablet or phone; only for signed-in people or only for guests; from and until a time; only in some languages; printing, page break, an anchor for links (`#name`) and the order. *Readers can close it* turns a block into a notice bar with a close button (named after its first heading): closed, it stays closed in this browser until its text changes; the focus goes on to what follows. A *Name for screen readers* makes the block a region with that name, so screen reader users find and skip it.

  Showing a block only to some is a rule of display, not a permission: the block loads its data with the permissions of the reader as always.
- **Size and spacing:** maximum width, minimum height, *Scrolls in itself from a height of* (the block then scrolls on its own, is reached with the Tab key and needs a name for screen readers), *Stays in view while its area scrolls* (on the desktop only, only as the last block of its area and only while it fits on the screen, so it never hides anything of its area), aspect ratio, inner and outer spacing and text alignment, per view where it makes sense. The tablet takes the value of the desktop, the phone the one of the tablet, unless they have their own.
- **Background:** colour, gradient, picture of the wiki (focus, size, repeat, parallax), video of the media library (silent, in a loop, always with a button to stop it; with "reduce motion" it does not start by itself; like a picture it needs an overlay), overlay, blending, pattern, glass.
- **Border, shape and dividers:** border (on all sides or only some: top, bottom, start or end of the line), rounded corners (all or only top, bottom, start or end), shadow, glow, dividers at the top and bottom (wave, zigzag, slope, arc), fading out at the edge.
- **Text:** font, size, weight, line height, letter spacing, capitals, text colour, columns, hyphenation, balanced line breaks, drop cap, text shadow, limited lines with *Show more*, headings in the gradient, underlined links with the style and colour of the underline (links always stay underlined).
- **Motion:** *Appear when scrolled into view* – fade in, rise from below, slide in from the start or the end of the line, or grow. Nothing moves when your system asks for less motion, nor in browsers that cannot tie the motion to scrolling; there the block is simply shown, and nothing is ever hidden.

The wiki checks every value; a sample shows the look at once:
- a block with a background of its own needs a text colour of its own with enough contrast (4.5:1);
- text on a picture needs an overlay; the text colour must be lighter or darker than every part of the picture under it (the same holds for gradients);
- columns of text need room in every view (each at least 12 rem; on a phone one column);
- a block that scrolls in itself needs a name for screen readers; sides of the border need a border and single rounded corners a radius.

Links in such a block keep its text colour and are underlined.

The same options are on **every block in the content** of a page (section, grid, notice box, cards …): in the dialog of the block, *Options*. Options the wiki does not accept are not used; the block then says why.

A page is made of blocks too: the title, the buttons to manage it, *Watch*, *Share*, *Favorite*, the page information (tags, author, revision), properties, reactions and comments. They sit at the positions of a page: header, toolbar, side column and below the content. The administration arranges them for all pages, and under *Administration → Placement → Pages by element type* also for the pages of one type (for example blog posts with their preview picture and a title bar, events without a side column); a category arranges them for its pages. For such layouts there is also the block *Preview picture of the page*. You arrange a single page in edit mode with *Arrange page*, for example without a visible title (screen readers keep it) or with *Share* below the content. This needs the permission to edit the page; for a blog, a FAQ and similar elements the permission to manage it. The controls of blog and FAQ are blocks on their page too: search, *New post*, *New category*, *Expand all / collapse all*, *New question*, *New section* and the feed. When a blog or FAQ is embedded in another page (for example as the start page of a category), it shows its controls itself. The buttons to manage the page always stay reachable. A copy of the page keeps its arrangement and its labels.

**Title bar:** the path and the title of a page or category are blocks of their own (*Path of the page*, *Title of the page*). The block *Title bar* shows both in one band; when it is placed, the two single blocks are left out, so nothing is shown twice. With its options the title bar gets a background (colour, gradient or picture with checked contrast). Placed for a category, this also applies to its subcategories; so every area has its own title bar.

The editing page can be arranged too (for the site and per category): notices, formatting toolbar, tags, properties, original text, comment on the change and the buttons *Publish*, *Draft*, *Preview* … sit in the header, above or below the content, in a side column or at the end of the page. Title and content always stay. Notices, properties and the buttons to save cannot be hidden, only moved. Which **buttons the formatting toolbar** shows is chosen by the administration for the site (*Administration → Settings*) and by whoever manages a category for it and its subcategories (*Category settings*: as above, all or a choice). Hidden formatting stays possible with shortcuts and Markdown.

## Permissions of a category

With the permission *Manage* you set under *Category settings → Permissions* who may do what: user, group, role, all signed-in people or guests. Choose a template (**Reader**, **Category editor**, **Category manager**) or single permissions, also the permissions of plugins (e.g. *Create workflows*). Optionally a rule also applies to all sub-categories. Inherited rules are shown with their origin. Details: [docs/en/admin/security.md](../admin/security.md#permissions).

**Permissions of an instance (blog, FAQ …):** A blog or a FAQ has permissions of its own, independent of the permissions for pages in the category: **manage** (settings, sections and permissions), **edit all content** and **contribute content** (new and own). On the page of the instance, **Permissions** opens the rules; whoever manages the instance grants them to people, groups, roles or all signed-in people, everybody else sees their own permissions there. Managers of the category always have all permissions of its instances; the same permissions can also be granted in the permissions of the category for all instances in it. Nobody grants permissions they do not have themselves. In archived categories, instance permissions do not apply.

**Labels of an instance:** whoever manages a blog or a FAQ changes the texts of its controls (for example "New post" or "Ask a question") under **Labels** – per active language. An empty field keeps the usual text.

With these permissions you write in the blog or FAQ even without page permissions in the category: **New post** opens the editor on behalf of the blog. Whoever may *contribute content* changes and deletes their own posts, whoever may *edit all content* all posts created through the blog (not other pages in the category). New sections and the settings of a blog or FAQ need *manage*; page permissions in the category are not enough for that.

## Signing in

Depending on the setup you sign in with user name and password (local account or LDAP/Active Directory) or with a button **Sign in with …** through the central login of your organisation (OpenID Connect or Shibboleth/SAML). If you have set up further factors (or the administration requires them), the form then asks for them step by step, e.g. for the code from your app; if you lack a required factor, you set it up right there. With a **passkey** you sign in, where allowed, completely without a password.

**Forgot your password:** If the administrators offer it and you have a local account, choose **Forgot your password?** on the sign-in page and enter your e-mail address. You get a link that is valid for one hour. Afterwards you are signed out everywhere and sign in with the new password.

**Create an account yourself:** If the administrators allow it, the sign-in page shows **Create an account**. Enter your e-mail address, your name and a password. You get an e-mail with a link that is valid for 48 hours. Your account is created only with this link; then you sign in with your e-mail address and the password. If the address already has an account, the e-mail says so.

## My profile

- Display name and preferred language, **e-mail addresses** (several possible, one of them the main address) and **linked sign-ins**,
- change password (local accounts), optionally enter a **user name** and under **Sign-in factors** see your own factor chain and set up factors: authenticator app, code by e-mail, **security key** or **passkey**, **allowed networks**,
- **Sessions**: where you are signed in; end single sessions or "Sign out everywhere else",
- **API keys** for scripts: choose permissions, category and expiry date. The key is only shown once,
- **SSH keys** for passwordless sign-in to the API,
- **Business card** (photo, function, department, phone, office, "About me") for team pages. Who sees your photo (only you, signed-in people or everyone) you choose within the limits set by the administration,
- **Watches**, **assigned watches** and **feed and calendar links**,
- **E-mail notifications** (also for mentions and shared pages),
- **Hint on long sentences:** only if the site offers it. *As the site decides* follows the setting of the administration; *On* or *Off* is your own choice (also `ewiki pref set readability on --as NAME`).
- **Show help texts:** show or hide explanations of fields and settings (default: off). The **?** button at the top of every page switches the same; without signing in, the browser remembers the choice. Notes you need to fill in a field correctly (formats, limits, required values, warnings) are always there. Screen readers do not read hidden explanations either. The button is a block of the placement (*Help texts (?)*).

## People

Wherever a person is named (under a page, in the history, in comments and drafts, in cases of processes, as the author of posts and events, on team pages and in the people blocks, in form submissions, access requests, the trash and the administration), the name is a button – if you may open this person. It opens a menu:

- **Open profile:** the page of the person (`/people/…`) with the business card: photo (as the person allows), function, department, phone, mobile, office, "About me".
- **Send e-mail:** only when you may see the address (see below). It opens your e-mail program.
- Plugins can add entries, e.g. a call.

With the keyboard: Tab to the name, Enter, Space or the arrow keys open the menu, the arrow keys, Home and End move, Escape closes it and you are back on the name. On a touch screen a tap opens it.

You may open the people you may find: yourself, the members of your own groups and – with the permission *List all users* – everybody. The administration can take the permission *Open people* away from everybody or give it only to certain roles. Where you may not open a person, the name is plain text. Guests never see people.

**Your e-mail address:** the administration decides who sees addresses (everybody signed in, only with a permission, or nobody). If it allows it, you can hide your own address in your profile (*Business card* → *Hide my e-mail address*); then nobody else sees it on your page and in the menu. Team pages are not affected: whoever builds a team page chooses there what it shows.

## Accessibility

The interface can be operated completely by keyboard (Tab, Shift+Tab, Enter, Space, Esc closes dialogs and menus). With **Skip to content** (first Tab key) you skip the navigation. All controls are labelled for screen readers, status messages are announced, and after a page change the focus jumps to the heading.

**Choosing a date:** next to every date field, the button with the calendar opens the date picker. Screen readers hear every day in full, for example “Wednesday, 25 November 2026”, and the new month when it changes. Keys: arrow keys move by day and week, Page Up and Page Down by month, with Shift by year; Enter chooses, Esc closes without a change. Days that cannot be chosen are announced as not available. You can also type the date into the field.
