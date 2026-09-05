"""Lay out Persian thesis lists with stable left/page and right/title columns."""

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
DOTS = "." * 60

WD_DO_NOT_SAVE_CHANGES = 0
WD_ALIGN_PARAGRAPH_LEFT = 0
WD_ALIGN_PARAGRAPH_CENTER = 1
WD_ALIGN_PARAGRAPH_RIGHT = 2


def visible_text(paragraph) -> str:
    return paragraph.Range.Text.replace("\r", "").replace("\x07", "").strip()


def find_exact(document, text: str):
    matches = [
        document.Paragraphs(index)
        for index in range(1, document.Paragraphs.Count + 1)
        if visible_text(document.Paragraphs(index)) == text
    ]
    if len(matches) != 1:
        raise RuntimeError(f"Expected one occurrence of {text!r}; found {len(matches)}")
    return matches[0]


def first_page_break_after(document, start: int):
    for index in range(1, document.Paragraphs.Count + 1):
        paragraph = document.Paragraphs(index)
        if paragraph.Range.Start > start and "\x0c" in paragraph.Range.Text:
            return paragraph
    raise RuntimeError("Could not find the page break that follows a list")


def chapter_title(raw: str) -> str:
    return raw.replace("\x0b", ": ").strip()


def toc_targets(document):
    items = []
    for index in range(1, document.Paragraphs.Count + 1):
        paragraph = document.Paragraphs(index)
        value = visible_text(paragraph)
        if not value:
            continue
        style = str(paragraph.Range.Style)
        if value.startswith(CHAPTER_PREFIX) and "\x0b" in value:
            items.append((chapter_title(value), 1, paragraph))
        elif style == "Heading 2":
            items.append((value, 2, paragraph))
        elif style == "Heading 1":
            items.append((value, 1, paragraph))
    return items


def list_entries(document, prefix: str):
    return [
        document.Paragraphs(index)
        for index in range(1, document.Paragraphs.Count + 1)
        if visible_text(document.Paragraphs(index)).startswith(prefix)
        and "\t" in visible_text(document.Paragraphs(index))
    ]


def body_captions(document, prefix: str):
    result = []
    for index in range(1, document.Paragraphs.Count + 1):
        paragraph = document.Paragraphs(index)
        value = visible_text(paragraph)
        if str(paragraph.Range.Style) == "Caption1" and value.startswith(prefix) and "\t" not in value:
            result.append((value, paragraph))
    return result


def set_font(range_, size: float, *, bold: bool = False) -> None:
    font = range_.Font
    font.Name = "B Nazanin"
    font.NameBi = "B Nazanin"
    font.Size = size
    font.SizeBi = size
    font.Bold = bold
    font.Italic = False
    font.Color = 0


def set_cell(cell, text: str, *, alignment: int, size: float, bold: bool = False, rtl: bool = False) -> None:
    cell.Range.Text = text
    paragraph = cell.Range.Paragraphs(1)
    set_font(paragraph.Range, size, bold=bold)
    fmt = paragraph.Range.ParagraphFormat
    fmt.Alignment = alignment
    fmt.ReadingOrder = 1 if rtl else 0
    fmt.SpaceBefore = 0
    fmt.SpaceAfter = 0
    fmt.LeftIndent = 0
    fmt.RightIndent = 0
    fmt.FirstLineIndent = 0
    cell.VerticalAlignment = 1


def build_table(document, start: int, end: int, row_count: int):
    """Replace plain list rows with an invisible physical three-column table."""
    document.Range(start, end).Delete()
    insertion = document.Range(start, start)
    table = document.Tables.Add(insertion, row_count, 3)
    table.Borders.Enable = 0
    table.AllowAutoFit = False
    section = insertion.Sections(1)
    setup = section.PageSetup
    available = float(setup.PageWidth - setup.LeftMargin - setup.RightMargin)
    page_width = 42.0
    dots_width = min(145.0, available - page_width - 190.0)
    title_width = available - page_width - dots_width
    table.Columns(1).Width = page_width
    table.Columns(2).Width = dots_width
    table.Columns(3).Width = title_width
    table.Rows.Alignment = WD_ALIGN_PARAGRAPH_LEFT
    for row in range(1, row_count + 1):
        table.Rows(row).AllowBreakAcrossPages = False
    return table


def fill_data_row(table, row: int, title: str, page: int, *, level: int = 1) -> None:
    set_cell(table.Cell(row, 1), str(page).translate(PERSIAN_DIGITS), alignment=WD_ALIGN_PARAGRAPH_LEFT, size=12, rtl=True)
    set_cell(table.Cell(row, 2), DOTS, alignment=WD_ALIGN_PARAGRAPH_RIGHT, size=12, rtl=False)
    set_cell(
        table.Cell(row, 3),
        title,
        alignment=WD_ALIGN_PARAGRAPH_RIGHT,
        size=12,
        bold=(level == 1),
        rtl=True,
    )


def fill_toc_table(table, targets, *, placeholders: bool) -> None:
    set_cell(table.Cell(1, 1), "\u0635\u0641\u062d\u0647", alignment=WD_ALIGN_PARAGRAPH_LEFT, size=14, bold=True, rtl=True)
    set_cell(table.Cell(1, 2), "", alignment=WD_ALIGN_PARAGRAPH_CENTER, size=14, bold=True)
    set_cell(table.Cell(1, 3), "\u0639\u0646\u0648\u0627\u0646", alignment=WD_ALIGN_PARAGRAPH_RIGHT, size=14, bold=True, rtl=True)
    table.Rows(1).HeadingFormat = True
    for row, (title, level, source) in enumerate(targets, 2):
        fill_data_row(table, row, title, 0 if placeholders else source.Range.Information(3), level=level)


def fill_caption_table(table, captions, *, placeholders: bool) -> None:
    for row, (title, source) in enumerate(captions, 1):
        fill_data_row(table, row, title, 0 if placeholders else source.Range.Information(3), level=2)


def normalise_toc_heading(heading) -> None:
    set_font(heading.Range, 16, bold=True)
    fmt = heading.Range.ParagraphFormat
    fmt.Alignment = WD_ALIGN_PARAGRAPH_CENTER
    fmt.ReadingOrder = 1
    fmt.SpaceBefore = 0
    fmt.SpaceAfter = 10
    fmt.LeftIndent = 0
    fmt.RightIndent = 0


def main() -> None:
    if not DOCX.is_file():
        raise FileNotFoundError(DOCX)

    word = document = None
    try:
        word = win32com.client.DispatchEx("Word.Application")
        word.Visible = False
        word.DisplayAlerts = 0
        document = word.Documents.Open(str(DOCX), ConfirmConversions=False, ReadOnly=False, AddToRecentFiles=False, Visible=False)

        targets = toc_targets(document)
        figures = body_captions(document, FIGURE_PREFIX)
        tables = body_captions(document, TABLE_PREFIX)
        if len(targets) != 46 or len(figures) != 5 or len(tables) != 12:
            raise RuntimeError(f"Unexpected list source counts: toc={len(targets)}, figures={len(figures)}, tables={len(tables)}")

        toc_heading = find_exact(document, TOC_TITLE)
        toc_break = first_page_break_after(document, toc_heading.Range.Start)
        toc_table = build_table(document, toc_heading.Range.End, toc_break.Range.Start, len(targets) + 1)
        normalise_toc_heading(toc_heading)
        fill_toc_table(toc_table, targets, placeholders=True)

        figure_rows = list_entries(document, FIGURE_PREFIX)
        figure_break = first_page_break_after(document, figure_rows[0].Range.Start)
        figure_table = build_table(document, figure_rows[0].Range.Start, figure_break.Range.Start, len(figures))
        fill_caption_table(figure_table, figures, placeholders=True)

        table_rows = list_entries(document, TABLE_PREFIX)
        table_break = first_page_break_after(document, table_rows[0].Range.Start)
        table_table = build_table(document, table_rows[0].Range.Start, table_break.Range.Start, len(tables))
        fill_caption_table(table_table, tables, placeholders=True)

        document.Save()
        document.Repaginate()

        final_targets = toc_targets(document)
        final_figures = body_captions(document, FIGURE_PREFIX)
        final_tables = body_captions(document, TABLE_PREFIX)
        if [item[0] for item in final_targets] != [item[0] for item in targets]:
            raise RuntimeError("The ToC targets changed during layout")
        if [item[0] for item in final_figures] != [item[0] for item in figures]:
            raise RuntimeError("The figure captions changed during layout")
        if [item[0] for item in final_tables] != [item[0] for item in tables]:
            raise RuntimeError("The table captions changed during layout")

        fill_toc_table(toc_table, final_targets, placeholders=False)
        fill_caption_table(figure_table, final_figures, placeholders=False)
        fill_caption_table(table_table, final_tables, placeholders=False)
        document.Save()
    finally:
        if document is not None:
            document.Close(SaveChanges=WD_DO_NOT_SAVE_CHANGES)
        if word is not None:
            word.Quit()

    print("Replaced all three lists with stable borderless title/leader/page tables.")


if __name__ == "__main__":
    main()
