"""Update the static thesis contents list after figures change pagination."""

from __future__ import annotations

from pathlib import Path

import win32com.client


DOCX = Path(__file__).resolve().parents[1] / "BudgetChain_BSc_Thesis_University_Style_Footnotes_Final.docx"
ENTRIES = (
    ("فصل اول: مقدمه", 11),
    ("فصل دوم: مفاهیم و مبانی نظری", 15),
    ("فصل سوم: تحلیل، طراحی و معماری پروژه", 22),
    ("فصل چهارم: پیاده‌سازی، آزمون و ارزیابی", 33),
    ("فصل پنجم: نتیجه‌گیری و پیشنهادهای آینده", 40),
    ("پیوست الف: راهنمای اجرای نمونه‌ی مفهومی", 42),
    ("پیوست ب: ماتریس داده‌ی قابل انتشار", 44),
    ("منابع", 46),
)
WD_DO_NOT_SAVE_CHANGES = 0


def update_entry(document, title: str, page: int) -> None:
    candidates = []
    for index in range(1, document.Paragraphs.Count + 1):
        paragraph = document.Paragraphs(index)
        value = paragraph.Range.Text.replace("\r", "").replace("\x07", "").strip()
        if value.startswith(title + "\t"):
            candidates.append(paragraph)
    if len(candidates) != 1:
        raise RuntimeError(f"Expected one contents entry for {title!r}; found {len(candidates)}")

    paragraph = candidates[0]
    raw = paragraph.Range.Text
    tab = raw.rfind("\t")
    if tab < 0:
        raise RuntimeError(f"No page-number tab found for {title!r}")
    page_range = document.Range(paragraph.Range.Start + tab + 1, paragraph.Range.End - 1)
    page_range.Text = str(page)


def main() -> None:
    word = document = None
    try:
        word = win32com.client.DispatchEx("Word.Application")
        word.Visible = False
        word.DisplayAlerts = 0
        document = word.Documents.Open(str(DOCX), ConfirmConversions=False, ReadOnly=False, AddToRecentFiles=False, Visible=False)
        for title, page in ENTRIES:
            update_entry(document, title, page)
        document.Save()
    finally:
        if document is not None:
            document.Close(SaveChanges=WD_DO_NOT_SAVE_CHANGES)
        if word is not None:
            word.Quit()
    print(f"Updated {len(ENTRIES)} static table-of-contents page numbers.")


if __name__ == "__main__":
    main()
