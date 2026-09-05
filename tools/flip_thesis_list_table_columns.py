"""Correct physical columns of borderless list tables in the RTL thesis file."""

from __future__ import annotations

from pathlib import Path

import win32com.client


ROOT = Path(__file__).resolve().parents[1]
DOCX = ROOT / "BudgetChain_BSc_Thesis_University_Style_Footnotes_Final.docx"

WD_DO_NOT_SAVE_CHANGES = 0
WD_ALIGN_PARAGRAPH_LEFT = 0
WD_ALIGN_PARAGRAPH_CENTER = 1
WD_ALIGN_PARAGRAPH_RIGHT = 2


def text(cell) -> str:
    return cell.Range.Text.replace("\r", "").replace("\x07", "").strip()


def set_cell(cell, value: str, *, alignment: int, size: float, bold: bool = False, rtl: bool = False) -> None:
    cell.Range.Text = value
    paragraph = cell.Range.Paragraphs(1)
    font = paragraph.Range.Font
    font.Name = "B Nazanin"
    font.NameBi = "B Nazanin"
    font.Size = size
    font.SizeBi = size
    font.Bold = bold
    font.Italic = False
    font.Color = 0
    fmt = paragraph.Range.ParagraphFormat
    fmt.Alignment = alignment
    fmt.ReadingOrder = 1 if rtl else 0
    fmt.SpaceBefore = 0
    fmt.SpaceAfter = 0
    fmt.LeftIndent = 0
    fmt.RightIndent = 0
    fmt.FirstLineIndent = 0
    cell.VerticalAlignment = 1


def is_list_table(table) -> bool:
    if table.Columns.Count != 3 or table.Rows.Count == 0:
        return False
    first_right_logical = text(table.Cell(1, 3))
    return (
        first_right_logical == "\u0639\u0646\u0648\u0627\u0646"
        or first_right_logical.startswith("\u0634\u06a9\u0644 ")
        or first_right_logical.startswith("\u062c\u062f\u0648\u0644 ")
    )


def flip(table, *, has_header: bool) -> None:
    # Tables inherit RTL direction from the Persian document; physical column 1
    # is therefore on the right. Give it the wide title column and put the
    # narrow page-number column in logical column 3 (the physical left side).
    old_width_1 = table.Columns(1).Width
    old_width_3 = table.Columns(3).Width
    table.Columns(1).Width = old_width_3
    table.Columns(3).Width = old_width_1

    for row in range(1, table.Rows.Count + 1):
        logical_1 = text(table.Cell(row, 1))
        logical_3 = text(table.Cell(row, 3))
        header = has_header and row == 1
        if header:
            set_cell(table.Cell(row, 1), logical_3, alignment=WD_ALIGN_PARAGRAPH_RIGHT, size=14, bold=True, rtl=True)
            set_cell(table.Cell(row, 3), logical_1, alignment=WD_ALIGN_PARAGRAPH_LEFT, size=14, bold=True, rtl=True)
        else:
            # Before this correction cell 1 contains the page and cell 3 the
            # title. Swapping them makes the visual result title-right/page-left.
            set_cell(table.Cell(row, 1), logical_3, alignment=WD_ALIGN_PARAGRAPH_RIGHT, size=12, bold=False, rtl=True)
            set_cell(table.Cell(row, 3), logical_1, alignment=WD_ALIGN_PARAGRAPH_LEFT, size=12, bold=False, rtl=True)
        middle = table.Cell(row, 2).Range.Paragraphs(1)
        middle.Range.ParagraphFormat.Alignment = WD_ALIGN_PARAGRAPH_RIGHT
        middle.Range.ParagraphFormat.ReadingOrder = 0


def main() -> None:
    word = document = None
    try:
        word = win32com.client.DispatchEx("Word.Application")
        word.Visible = False
        word.DisplayAlerts = 0
        document = word.Documents.Open(str(DOCX), ConfirmConversions=False, ReadOnly=False, AddToRecentFiles=False, Visible=False)
        candidates = [document.Tables(index) for index in range(1, document.Tables.Count + 1) if is_list_table(document.Tables(index))]
        if len(candidates) != 3:
            raise RuntimeError(f"Expected three list tables; found {len(candidates)}")
        for table in candidates:
            flip(table, has_header=text(table.Cell(1, 3)) == "\u0639\u0646\u0648\u0627\u0646")
        document.Save()
    finally:
        if document is not None:
            document.Close(SaveChanges=WD_DO_NOT_SAVE_CHANGES)
        if word is not None:
            word.Quit()
    print("Flipped list columns for RTL physical layout.")


if __name__ == "__main__":
    main()
