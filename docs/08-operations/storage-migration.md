# LatticeDB to Rhiza Migration

This procedure copies an existing Yeoul LatticeDB database into a new local
Rhiza directory. It never modifies the source. Released Yeoul v0.10.x and the
installed user-level database remain on LatticeDB unless an operator performs
and verifies this migration explicitly.

## Requirements

- Stop all Yeoul processes using the source database. The migrator also takes
  exclusive database ownership through copying and verification, and refuses a
  concurrent owner. It closes the source and checks close errors before
  publishing the target; stop source writers because this is a point-in-time
  copy, not an atomic application cutover.
- Choose a target path that does not exist. Source and target must not alias or
  contain one another.
- On Windows, migration is not supported yet: Yeoul returns
  `ErrNotSupported` before creating a marker, lock, or target because the
  current implementation cannot durably synchronize directory entries there.
- Rhiza storage is unavailable on Windows until pinned Rhiza v0.19.5's qlog
  package replaces its Unix-only `syscall.Flock` dependency. Yeoul isolates the
  native package so Windows builds continue with the Lattice fallback.

## Run

```sh
yeoul migrate-storage --source "$HOME/.local/share/yeoul/work-memory.ltdb" \
  --target "$HOME/.local/share/yeoul/work-memory-rhiza.ltdb"
```

After success, verify the target with `yeoul inspect counts --db
"$HOME/.local/share/yeoul/work-memory-rhiza.ltdb"` and representative `yeoul search`
queries, then explicitly configure the application to use that target. Keep the
source backup until the target has been verified in the intended workflow.

Keep the `.ltdb` extension for Rhiza databases too. The distinct target basename
above prevents overwriting the source; the storage format is detected from the
directory contents, not the extension.

The migrator creates and durably syncs a pending marker before creating the
target. It copies bounded batches, writes Yeoul sequence metadata last, checks
the full state (including revisions and lifecycle records), closes and reopens
the target, closes the source, checks cancellation and close errors, then removes
and syncs the marker to publish the result. A crash or
error before publication leaves the marker in place; the incomplete target is
refused on open and is not resumed or overwritten automatically. Preserve the
source and investigate or remove an incomplete target only after reviewing it.

Each migration batch is capped below Rhiza's graph mutation limit. Ordinary
Yeoul saves are different: each entire state delta is one atomic mutation and
is rejected if its encoded size exceeds 96 KiB. It is never divided into
multiple commits. The public Rhiza full-text predicate returns all matching
candidates subject to its result-size limit; it is not a dedicated accelerated
postings index, and its backend ranking score is not LatticeDB term-frequency
scoring. Historical `as_of` full-text queries remain unsupported.

Rhiza commits the changed-record key catalog in the same graph mutation as each
delta. Recovery reads this catalog in bounded stream pages and resolves keys
with indexed point lookups. Do not trim `yeoul.catalog.v1`: it is canonical
recovery metadata, and startup replay currently grows linearly with write
history. Recovery fails closed if the stream has a sequence gap or the catalog
does not match the records.
