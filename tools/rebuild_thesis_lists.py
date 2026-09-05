"""Rebuild the thesis contents, figures, and tables lists from Word's live pages."""

from __future__ import annotations

from pathlib import Path

import win32com.client


ROOT = Path(__file__).resolve().parents[1]
DOCX = ROOT / "BudgetChain_BSc_Thesis_University_Style_Footnotes_Final.docx"

TOC_TITLE = "\u0641\u0647\u0631\u0633\u062a \u0645\u0637\u0627\u0644\u0628"
FIGURE_PREFIX = "\u0634\u06a9\u0644 "
TABLE_PREFIX = "\u062c\u062f\u0648\u0644 "
CHAPTER_PREFIX = "\u0641\u0635\u0644 "
PERSIAN_DIGITS = str.maketrans("0123456789", "\u06f0\u06f1\u06f2\u06f3\u06f4\u06f5\u06f6\u06f7\u06f8\u06f9")

WD_DO_NOT_SAVE_CHANGES = 0
WD_ALIGN_PARAGRAPH_CENTER = 1
WD_ALIGN_PARAGRAPH_RIGHT = 2
WD_ALIGN_TAB_LEFT = 0
WD_TAB_LEADER_DOTS = 1
WD_TAB_LEADER_SPACES = 0


def visible_text(paragraph) -> str:
    return paragraph.Range.Text.replace("\r", "").replace("\x07", "").strip()


def set_text(paragraph, text: str) -> None:
    body = paragraph.Range.Duplicate
    body.End -= 1  # Keep the paragraph mark and its page-break properties.
    body.Text = text


def set_font(paragraph, size: float, *, bold: bool = False) -> None:
    font = paragraph.Range.Font
    font.Name = "B Nazanin"
    font.NameBi = "B Nazanin"
    font.Size = size
    font.SizeBi = size
    font.Bold = bold
    font.Italic = False
    font.Color = 0  # Black; the university template does not use blue headings.


def configure_row(paragraph, *, indent: float, leader: int) -> None:
    fmt = paragraph.Range.ParagraphFormat
    fmt.Alignment = WD_ALIGN_PARAGRAPH_RIGHT
    # A left tab advances to the next stop on the right in an LTR paragraph.
    # Lists are Persian: RTL makes the same tab travel toward the left page
    # column, where the dot leader and page number belong.
    fmt.ReadingOrder = 1
    fmt.RightIndent = indent
    fmt.LeftIndent = 0
    fmt.FirstLineIndent = 0
    fmt.SpaceBefore = 0
    fmt.SpaceAfter = 4
    fmt.LineSpacingRule = 0  # Single spacing.
    tabs = fmt.TabStops
    tabs.ClearAll()
    # The position is measured from the left text margin. It places page
    # numbers on the left and title text on the right in RTL paragraphs.
    tabs.Add(59.5, WD_ALIGN_TAB_LEFT, leader)


def first_page_break_after(document, start: int):
    for index in range(1, document.Paragraphs.Count + 1):
        paragraph = document.Paragraphs(index)
        if paragraph.Range.Start > start and "\x0c" in paragraph.Range.Text:
            return paragraph
    raise RuntimeError("Page break following a list was not found")


def find_exact(document, text: str):
    result = []
    for index in range(1, document.Paragraphs.Count + 1):
        paragraph = document.Paragraphs(index)
        if visible_text(paragraph) == text:
            result.append(paragraph)
    if len(result) != 1:
        raise RuntimeError(f"Expected exactly one paragraph {text!r}; found {len(result)}")
    return result[0]


def chapter_title(raw: str) -> str:
    return raw.replace("\x0b", ": ").strip()


def collect_toc_targets(document):
    """Return every chapter, numbered subsection, appendix, and references."""
    targets = []
    for index in range(1, document.Paragraphs.Count + 1):
        paragraph = document.Paragraphs(index)
        value = visible_text(paragraph)
        if not value:
            continue
        style = str(paragraph.Range.Style)
        if value.startswith(CHAPTER_PREFIX) and "\x0b" in value:
            targets.append((chapter_title(value), 1, paragraph))
        elif style == "Heading 2":
            targets.append((value, 2, paragraph))
        elif style == "Heading 1":
            targets.append((value, 1, paragraph))
    return targets


def list_entries(document, prefix: str):
    """Return existing entries in the front-matter list, excluding captions."""
    return [
        document.Paragraphs(index)
        for index in range(1, document.Paragraphs.Count + 1)
        if visible_text(document.Paragraphs(index)).startswith(prefix)
        and "\t" in visible_text(document.Paragraphs(index))
    ]


def body_captions(document, prefix: str):
    """Return caption paragraphs from the thesis body, in document order."""
    result = []
    for index in range(1, document.Paragraphs.Count + 1):
        paragraph = document.Paragraphs(index)
        value = visible_text(paragraph)
        if str(paragraph.Range.Style) == "Caption1" and value.startswith(prefix) and "\t" not in value:
            result.append((value, paragraph))
    return result


def replace_list_block(document, prefix: str, item_count: int) -> None:
    """Replace all current entries before their page break with placeholders."""
    existing = list_entries(document, prefix)
    if not existing:
        raise RuntimeError(f"No current list entries found for {prefix!r}")
    start = existing[0].Range.Start
    page_break = first_page_break_after(document, start)
    placeholders = [f"{prefix}\t\u06f0" for _ in range(item_count)]
    document.Range(start, page_break.Range.Start).Text = "\r".join(placeholders) + "\r"


def toc_paragraphs(document):
    heading = find_exact(document, TOC_TITLE)
    page_break = first_page_break_after(document, heading.Range.Start)
    paragraphs = []
    for index in range(1, document.Paragraphs.Count + 1):
        paragraph = document.Paragraphs(index)
        if heading.Range.End <= paragraph.Range.Start < page_break.Range.Start:
            paragraphs.append(paragraph)
    return heading, paragraphs


def rebuild_toc_placeholders(document, target_count: int) -> None:
    heading = find_exact(document, TOC_TITLE)
    page_break = first_page_break_after(document, heading.Range.Start)
    rows = ["\u0639\u0646\u0648\u0627\u0646\t\u0635\u0641\u062d\u0647"]
    rows.extend([f"\u0645\u062f\u062e\u0644\t\u06f0" for _ in range(target_count)])
    document.Range(heading.Range.End, page_break.Range.Start).Text = "\r".join(rows) + "\r"


def format_toc(document, targets) -> None:
    heading, rows = toc_paragraphs(document)
    if len(rows) != len(targets) + 1:
        raise RuntimeError(f"Expected {len(targets) + 1} ToC rows; found {len(rows)}")

    set_text(heading, TOC_TITLE)
    set_font(heading, 16, bold=True)
    fmt = heading.Range.ParagraphFormat
    fmt.Alignment = WD_ALIGN_PARAGRAPH_CENTER
    fmt.SpaceBefore = 0
    fmt.SpaceAfter = 10
    fmt.LeftIndent = 0
    fmt.RightIndent = 0

    label = rows[0]
    set_text(label, "\u0639\u0646\u0648\u0627\u0646\t\u0635\u0641\u062d\u0647")
    set_font(label, 14, bold=True)
    configure_row(label, indent=0, leader=WD_TAB_LEADER_SPACES)
    label.Range.ParagraphFormat.SpaceAfter = 5

    for paragraph, (title, level, source) in zip(rows[1:], targets, strict=True):
        page = source.Range.Information(3)
        set_text(paragraph, f"{title}\t{str(page).translate(PERSIAN_DIGITS)}")
        set_font(paragraph, 12.5, bold=(level == 1))
        configure_row(paragraph, indent=(0 if level == 1 else 20), leader=WD_TAB_LEADER_DOTS)


def format_caption_list(document, prefix: str, captions) -> None:
    entries = list_entries(document, prefix)
    if len(entries) != len(captions):
        raise RuntimeError(f"Expected {len(captions)} entries for {prefix!r}; found {len(entries)}")
    for paragraph, (title, source) in zip(entries, captions, strict=True):
        page = source.Range.Information(3)
        set_text(paragraph, f"{title}\t{str(page).translate(PERSIAN_DIGITS)}")
        set_font(paragraph, 12, bold=False)
        configure_row(paragraph, indent=0, leader=WD_TAB_LEADER_DOTS)


def main() -> None:
    if not DOCX.is_file():
        raise FileNotFoundError(DOCX)

    word = document = None
    try:
        word = win32com.client.DispatchEx("Word.Application")
        word.Visible = False
        word.DisplayAlerts = 0
        document = word.Documents.Open(str(DOCX), ConfirmConversions=False, ReadOnly=False, AddToRecentFiles=False, Visible=False)

        toc_targets = collect_toc_targets(document)
        if len(toc_targets) < 40:
            raise RuntimeError(f"Too few ToC targets: {len(toc_targets)}")
        figure_captions = body_captions(document, FIGURE_PREFIX)
        table_captions = body_captions(document, TABLE_PREFIX)
        if not figure_captions or not table_captions:
            raise RuntimeError("Body figure or table captions were not found")

        rebuild_toc_placeholders(document, len(toc_targets))
        replace_list_block(document, FIGURE_PREFIX, len(figure_captions))
        replace_list_block(document, TABLE_PREFIX, len(table_captions))

        # These insertions can move every subsequent target. Save and repaginate
        # before collecting the final page numbers used in all three lists.
        document.Save()
        document.Repaginate()

        final_toc_targets = collect_toc_targets(document)
        if [item[0] for item in final_toc_targets] != [item[0] for item in toc_targets]:
            raise RuntimeError("ToC targets changed unexpectedly while rebuilding")
        final_figures = body_captions(document, FIGURE_PREFIX)
        final_tables = body_captions(document, TABLE_PREFIX)
        if [item[0] for item in final_figures] != [item[0] for item in figure_captions]:
            raise RuntimeError("Figure captions changed unexpectedly while rebuilding")
        if [item[0] for item in final_tables] != [item[0] for item in table_captions]:
            raise RuntimeError("Table captions changed unexpectedly while rebuilding")

        format_toc(document, final_toc_targets)
        format_caption_list(document, FIGURE_PREFIX, final_figures)
        format_caption_list(document, TABLE_PREFIX, final_tables)

        # Direct formatting of the expanded contents list can itself alter the
        # number of front-matter pages. Recalculate once more, then write the
        # now-stable page numbers. The final rewrite changes only digits.
        document.Save()
        document.Repaginate()
        format_toc(document, collect_toc_targets(document))
        format_caption_list(document, FIGURE_PREFIX, body_captions(document, FIGURE_PREFIX))
        format_caption_list(document, TABLE_PREFIX, body_captions(document, TABLE_PREFIX))
        document.Save()
    finally:
        if document is not None:
            document.Close(SaveChanges=WD_DO_NOT_SAVE_CHANGES)
        if word is not None:
            word.Quit()

    print("Rebuilt contents, figures, and tables lists from live Word page numbers.")


if __name__ == "__main__":
    main()
