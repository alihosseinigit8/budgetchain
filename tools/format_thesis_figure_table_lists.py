"""Fill figure/table lists with page numbers and apply the template headers."""

from __future__ import annotations

from pathlib import Path

import win32com.client


ROOT = Path(__file__).resolve().parents[1]
DOCX = ROOT / "BudgetChain_BSc_Thesis_University_Style_Footnotes_Final.docx"
TEMPLATE = Path(r"C:\Users\User\Downloads\BScProjectTemplate (1) - Copy.docx")

FIGURES = (
    ("شکل ۲-۱. جایگاه دفترکل مجوزدار در فرایند سازمانی", 16),
    ("شکل ۳-۱. معماری چهارشبکه‌ای BudgetChain", 25),
    ("شکل ۳-۲. مرز داده میان شبکه‌ی مالیاتی و هسته‌ی بودجه", 27),
    ("شکل ۳-۳. توالی امضای درخواست تا آزادسازی اعتبار", 29),
    ("شکل ۴-۱. نمایش پیشنهادی سناریوی اجرای نمونه", 35),
)
TABLES = (
    ("جدول ۲-۱. مقایسه‌ی مدل‌های شبکه برای مسئله‌ی پروژه", 16),
    ("جدول ۲-۲. مقایسه‌ی رویکردهای مرتبط با نمونه‌ی حاضر", 20),
    ("جدول ۳-۱. ذی‌نفعان و نقش‌های سامانه", 22),
    ("جدول ۳-۲. نیازمندی‌های کارکردی پروژه", 23),
    ("جدول ۳-۳. مرز داده در چهار دفترکل", 25),
    ("جدول ۳-۴. نگاشت قانون‌های پروژه به ماژول‌های پیاده‌سازی", 30),
    ("جدول ۴-۱. محیط و اجزای پیاده‌سازی", 33),
    ("جدول ۴-۲. نتایج سناریوی آزمون", 36),
    ("جدول ۴-۳. ارزیابی تحقق نیازمندی‌ها و محدودیت‌ها", 37),
)

WD_HEADER_FOOTER_PRIMARY = 1
WD_HEADER_FOOTER_FIRST_PAGE = 2
WD_SECTION_BREAK_CONTINUOUS = 3
WD_COLLAPSE_START = 1
WD_ALIGN_PARAGRAPH_RIGHT = 2
WD_ALIGN_TAB_LEFT = 0
WD_TAB_LEADER_DOTS = 1
WD_DO_NOT_SAVE_CHANGES = 0
PERSIAN_DIGITS = str.maketrans("0123456789", "۰۱۲۳۴۵۶۷۸۹")


def visible_text(paragraph) -> str:
    return paragraph.Range.Text.replace("\r", "").replace("\x07", "").strip()


def find_heading(document, expected: str):
    matches = [document.Paragraphs(index) for index in range(1, document.Paragraphs.Count + 1) if visible_text(document.Paragraphs(index)) == expected]
    if len(matches) != 1:
        raise RuntimeError(f"Expected one heading {expected!r}; found {len(matches)}")
    return matches[0]


def caption_entries_between(document, start_heading, end_heading):
    entries = []
    for index in range(1, document.Paragraphs.Count + 1):
        paragraph = document.Paragraphs(index)
        if start_heading.Range.Start < paragraph.Range.Start < end_heading.Range.Start and str(paragraph.Range.Style) == "Caption1":
            entries.append(paragraph)
    return entries


def insert_continuous_break_before(paragraph) -> None:
    position = paragraph.Range.Duplicate
    position.Collapse(WD_COLLAPSE_START)
    position.InsertBreak(WD_SECTION_BREAK_CONTINUOUS)


def section_for_paragraph(paragraph):
    return paragraph.Range.Sections(1)


def copy_primary_header(target_section, source_header_range) -> None:
    target_section.PageSetup.DifferentFirstPageHeaderFooter = False
    target_header = target_section.Headers(WD_HEADER_FOOTER_PRIMARY)
    target_header.LinkToPrevious = False
    target_header.Range.FormattedText = source_header_range.FormattedText


def clear_primary_header(section) -> None:
    section.PageSetup.DifferentFirstPageHeaderFooter = False
    header = section.Headers(WD_HEADER_FOOTER_PRIMARY)
    header.LinkToPrevious = False
    header.Range.Text = ""


def blank_body_heading(paragraph) -> None:
    body_range = paragraph.Range.Duplicate
    body_range.End -= 1  # Retain the paragraph mark and its page-break layout.
    body_range.Text = ""


def set_list_entry(paragraph, caption: str, page: int) -> None:
    content = paragraph.Range.Duplicate
    content.End -= 1  # retain paragraph mark
    content.Text = f"{caption}\t{str(page).translate(PERSIAN_DIGITS)}"
    paragraph.Range.Font.Name = "B Nazanin"
    paragraph.Range.Font.NameBi = "B Nazanin"
    paragraph.Range.Font.Size = 11
    paragraph.Range.Font.SizeBi = 11
    paragraph.Range.Font.Bold = False
    paragraph.Range.Font.Italic = False
    paragraph.Range.ParagraphFormat.Alignment = WD_ALIGN_PARAGRAPH_RIGHT
    tabs = paragraph.Range.ParagraphFormat.TabStops
    tabs.ClearAll()
    tabs.Add(59.5, WD_ALIGN_TAB_LEFT, WD_TAB_LEADER_DOTS)


def main() -> None:
    if not DOCX.is_file() or not TEMPLATE.is_file():
        raise FileNotFoundError("Final thesis or template file is missing")

    word = document = template = None
    try:
        word = win32com.client.DispatchEx("Word.Application")
        word.Visible = False
        word.DisplayAlerts = 0
        template = word.Documents.Open(str(TEMPLATE), ConfirmConversions=False, ReadOnly=True, AddToRecentFiles=False, Visible=False)
        document = word.Documents.Open(str(DOCX), ConfirmConversions=False, ReadOnly=False, AddToRecentFiles=False, Visible=False)

        figures_heading = find_heading(document, "فهرست شکل‌ها")
        tables_heading = find_heading(document, "فهرست جدول‌ها")
        abbreviations_heading = find_heading(document, "فهرست اختصارات")
        figure_entries = caption_entries_between(document, figures_heading, tables_heading)
        table_entries = caption_entries_between(document, tables_heading, abbreviations_heading)
        if len(figure_entries) != len(FIGURES):
            raise RuntimeError(f"Expected {len(FIGURES)} figure entries; found {len(figure_entries)}")
        if len(table_entries) != len(TABLES):
            raise RuntimeError(f"Expected {len(TABLES)} table entries; found {len(table_entries)}")

        # Page breaks already precede these headings. Continuous section breaks
        # let each list receive its own header without introducing blank pages.
        for paragraph in (abbreviations_heading, tables_heading, figures_heading):
            insert_continuous_break_before(paragraph)

        figures_heading = find_heading(document, "فهرست شکل‌ها")
        tables_heading = find_heading(document, "فهرست جدول‌ها")
        abbreviations_heading = find_heading(document, "فهرست اختصارات")
        copy_primary_header(
            section_for_paragraph(figures_heading),
            template.Sections(3).Headers(WD_HEADER_FOOTER_FIRST_PAGE).Range,
        )
        copy_primary_header(
            section_for_paragraph(tables_heading),
            template.Sections(4).Headers(WD_HEADER_FOOTER_FIRST_PAGE).Range,
        )
        clear_primary_header(section_for_paragraph(abbreviations_heading))

        figure_entries = caption_entries_between(document, figures_heading, tables_heading)
        table_entries = caption_entries_between(document, tables_heading, abbreviations_heading)
        for paragraph, (caption, page) in zip(figure_entries, FIGURES, strict=True):
            set_list_entry(paragraph, caption, page)
        for paragraph, (caption, page) in zip(table_entries, TABLES, strict=True):
            set_list_entry(paragraph, caption, page)

        blank_body_heading(figures_heading)
        blank_body_heading(tables_heading)
        document.Save()
    finally:
        if document is not None:
            document.Close(SaveChanges=WD_DO_NOT_SAVE_CHANGES)
        if template is not None:
            template.Close(SaveChanges=WD_DO_NOT_SAVE_CHANGES)
        if word is not None:
            word.Quit()
    print(f"Updated {len(FIGURES)} figure entries and {len(TABLES)} table entries; copied two template headers.")


if __name__ == "__main__":
    main()
