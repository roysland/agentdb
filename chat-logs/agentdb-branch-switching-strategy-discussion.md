Claude
Let me look at the watch command and the analyze/index pipeline to understand the relationship.

Let me look at the internal watch implementation to see exactly what runs with and without --analyze.

Now I have the full picture. Let me also check the watcher internals quickly.

watcher.go
There are actually two bugs here, not one:

watch never generates embeddings at all — reindex() in watcher.go creates chunks but never calls the embed provider (unlike index.go), and runAnalyze() stores symbols but never embeds them either. Every file save via watch silently produces un-embedded data.

--analyze defaulting to false is a bad default when embedding is configured, because analyze is also where symbol embeddings come from.

The fix is: default --analyze to true when embedding is configured (via cmd.Flags().Changed detection in RunE), and wire the embed provider into the watcher so it actually uses it.

Let me implement both:

go.mod
Now I'll make the changes. First the watcher — add embed provider support:Now wire embeddings into reindex for chunks:Now wire embeddings into runAnalyze for symbols:Now update cmd/watch.go to create the provider and pass it through, and default --analyze based on embedding config:Now verify it compiles:

Compiles clean (the diagnostics are pre-existing unused functions, not related to these changes). Here's what was fixed and why:

Bug 1 — watch never generated embeddings (silent data corruption)
watcher.go:reindex() was creating chunks without calling the embed provider. Every file save via watch left those chunks without embeddings, progressively degrading hybrid/semantic search over time as the index diverged from what agentdb index had originally produced. Same issue in runAnalyze — symbols were stored without embeddings.

Bug 2 — wrong default for --analyze when embedding is configured
Inside RunE, after resolving config, the code now checks cmd.Flags().Changed("analyze"). If you didn't explicitly pass the flag and embedding is enabled, analyze is set to true. The explicit --analyze=false escape hatch still works for anyone who consciously wants index-only watch on a large codebase where analyze is too slow.

