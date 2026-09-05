"""Remove the temporary ASCII markers left after Word inserts each figure."""

from __future__ import annotations

from pathlib import Path

import win32com.client


DOCX = Path(__file__).resolve().parents[1] / "BudgetChain_BSc_Thesis_University_Style_Footnotes_Final.docx"
MARKERS = ("[[FIGURE_2_1]]", "[[FIGURE_3_1]]", "[[FIGURE_3_2]]", "[[FIGURE_3_3]]", "[[FIGURE_4_1]]")
WD_FIND_STOP = 0
WD_DO_NOT_SAVE_CHANGES = 0


def remove_marker(document, marker: str) -> None:
    search_range = document.Content
    finder = search_range.Find
    finder.ClearFormatting()
    finder.Replacement.ClearFormatting()
    finder.MatchWildcards = False
    found = finder.Execute(FindText=marker, Forward=True, Wrap=WD_FIND_STOP, Format=False)
    if not found:
        raise RuntimeError(f"Could not find temporary marker {marker}")
    search_range.Text = ""


def main() -> None:
    word = document = None
    try:
        word = win32com.client.DispatchEx("Word.Application")
        word.Visible = False
        word.DisplayAlerts = 0
        document = word.Documents.Open(str(DOCX), ConfirmConversions=False, ReadOnly=False, AddToRecentFiles=False, Visible=False)
        for marker in MARKERS:
            remove_marker(document, marker)
        document.Save()
    finally:
        if document is not None:
            document.Close(SaveChanges=WD_DO_NOT_SAVE_CHANGES)
        if word is not None:
            word.Quit()
    print(f"Removed {len(MARKERS)} temporary figure markers.")


if __name__ == "__main__":
    main()
