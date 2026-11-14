"""MkDocs hooks.

docs/changelog.md is a symlink to CHANGELOG.md at the repository root, where
the release process and the habit of reading it expect it. The edit link
Material builds from edit_uri points at the symlink, and GitHub's editor opens
a symlink as a one-line file holding its target path, so editing from there
would replace the link rather than the document. Point the page at the file it
resolves to.
"""

import os


def on_page_context(context, page, config, nav):
    src = page.file.abs_src_path
    if not os.path.islink(src):
        return context
    target = os.path.relpath(os.path.realpath(src), start=os.path.dirname(config["docs_dir"]))
    page.edit_url = config["repo_url"] + "/edit/main/" + target
    context["page"] = page
    return context
