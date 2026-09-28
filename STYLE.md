# Go Style Guide

This guide favors **clear, simple, idiomatic Go**. Code is written primarily for readers, not for reducing line count.

## General

Always run `gofmt`. Do not manually fight its formatting.

Prefer simple, explicit code over clever abstractions or compressed expressions. Keep the normal execution path visually clear and handle exceptional cases early.

Avoid unnecessary abstraction. A little duplication is often preferable to introducing the wrong abstraction or dependency.

Design types so their zero value is useful whenever practical. ([Go][1])

## Naming

Names should be **concise, precise, and predictable**. Use the shortest name that communicates the required information in its context. A name farther from its declaration should generally contain more information. ([Gopher Guides][3])

Short names are appropriate for small scopes:

```go
for i := range items {
    item := items[i]
}
```

Long-lived or widely scoped identifiers should be more descriptive.

Do not encode types into names:

```go
// Bad
usersMap map[string]*User
nameString string

// Good
users map[string]*User
name string
```

Collections should normally use plural nouns:

```go
users     []User
companies map[string]*Company
```

Use `MixedCaps`, never snake_case. Preserve conventional capitalization of initialisms:

```go
userID
HTTPClient
URL
parseURL
```

Names should describe purpose or meaning, not implementation details. ([Google GitHub Pages][2])

Names should be consistent throughout the codebase, similar concepts should be name in the same
way.

```go
//Bad
type Scene struct{
    topo          []uint32
    topologyCount uint32
}

//Good
type Scene struct {
    topology      []uint32
    topologyCount uint32
}
```

### Names must stand on their own

A name has to be understood where it is **used**, with no comment and no declaration in view. Its writer always thinks it is clear — the context is in their head — so judge it at the call site:

```go
// Bad: timing what? enter what? read what?
if p.isTiming() {
    p.enter(cmd, GPUPassCull)
}
p.read(backend)

// Good
if p.isGPUProfilingEnabled() {
    p.beginPass(GPUPassCull, cmd)
}
p.readGPUTimestamps(backend)
```

If a doc comment's first line mostly spells out the name — "isTiming reports whether GPU timestamps are being recorded" — the missing words belong in the name.

- A verb carries its object, unless the receiver is the only thing it could act on: `readGPUTimestamps`, not `read`; but `buf.Grow()`.
- Paired operations name the same object on both sides: `beginPass`/`endPass`, `enableGPUProfiling`/`disableGPUProfiling`.
- A quantity whose type does not carry its unit says it in the name: `gpuFrameMS`, `sizeBytes`. A `time.Duration` needs no suffix.
- Spell words out: `material`, not `mat`; `pipeline`, not `pipe`. Abbreviations are limited to established initialisms (`ID`, `GPU`, `CPU`, `FPS`, `LOD`, `MS`), and to well-established abbreviations, and to short receivers and loop indices in small scopes.

### Public API names

A public API should be understandable largely from its signature.

Parameter names must communicate what callers are expected to provide. Avoid opaque names merely because their type is obvious:

```go
// Bad
func Process(s string) error

// Good
func Process(username string) error
```

Conventional short names such as `ctx`, `r`, or `w` are acceptable when their meaning is established and unmistakable from the type and context.

If a function is difficult to name clearly, consider whether it has too many responsibilities. Functions should generally be named for what they return or compute; methods for the action they perform. ([Gopher Guides][3])

### Getters and Setters

Getter methods should usually be named after the value they return, without a Get prefix:

```go
// Bad
func (db *DB) GetUsers() []User

// Good
func (db *DB) Users() []User
```

For an unexported field such as users, the conventional exported getter is Users.

Setter methods conventionally use the Set prefix:

```go
func (db *DB) SetUsers(users []User)
```

Prefer:
```go
db.Users()
db.SetUsers(users)
```

over:

```go
db.GetUsers()
db.SetUsers(users)
```

Use Get only when it is part of the operation’s meaning rather than merely indicating property access.

### Boolean Predicates

Methods that return a boolean should read as predicates. Prefer names beginning with `Is`, `Has`, or another appropriate boolean verb when it improves clarity:

```go
// Bad
func (v Value) Valid() bool
func (n Node) Children() bool

// Good
func (v Value) IsValid() bool
func (n Node) HasChildren() bool
```

Choose the prefix according to the meaning:

```go
IsValid()
IsEmpty()
IsVisible()

HasChildren()
HasValue()
HasFocus()
```

The method name should read naturally as a yes/no question and make the boolean result obvious at the call site.

A prefix alone does not make a good predicate. A boolean names a **state**, not an activity:

```go
// Bad
func (p *Profiler) isTiming() bool
open, closed [passCount]bool

// Good
func (p *Profiler) isGPUProfilingEnabled() bool
passStarted, passEnded [passCount]bool
```

## Packages

Package names should describe **what the package provides**, not merely what it contains.

Use short, lowercase, single-word names without underscores or mixed caps:

```go
audio
render
geometry
stringset
```

Avoid meaningless catch-all packages such as:

```text
util
common
base
misc
helpers
```

Do not create unnecessary package taxonomies.

Remember that the package name is part of every exported identifier. Avoid repetition:

```go
// Bad
geometry.GeometryManager
audio.AudioDevice

// Better
geometry.Manager
audio.Device
```

Prefer:

```go
widget.New()
```

over:

```go
widget.NewWidget()
```

Do not choose a package name that steals an obvious variable name from callers. ([Go][1])

## Functions and Methods

Functions should do one coherent thing.

Prefer early returns over unnecessary nesting:

```go
if err != nil {
    return err
}

process()
```

rather than:

```go
if err == nil {
    process()
} else {
    return err
}
```

Do not write one-line function bodies. Even trivial functions use the normal multiline form:

```go
// Bad
func (c *Counter) Count() int { return c.count }

// Good
func (c *Counter) Count() int {
    return c.count
}
```

Use `defer` when it makes resource ownership and cleanup clearer. ([Go][1])

Avoid functions with many parameters of identical primitive types when their meaning becomes difficult to distinguish.

### Argument order

The subject comes first; context comes last. The subject is what the call is about; context is where or with what it happens — a command buffer, a backend, a store:

```go
// Bad
p.beginPass(cmd, pass)
r.encodeCull(cmd, st, v)

// Good
p.beginPass(pass, cmd)
r.encodeCull(v, st, cmd)
```

The only exception is when the argument order is a convention:

```go
//Bad
ctx := context.Background()
p.beginPass(cmd, pass, ctx)

// Good
ctx := context.Background()
p.beginPass(ctx, cmd, pass)
```

### Orchestration reads as a recipe

A function that coordinates a process is a short sequence of calls to named steps, and does no work of its own. Its steps are defined below it, in the order it calls them, so the file reads top to bottom as the process:

```go
func (r *Renderer) Render(scene scenes.Producer, cam Camera) {
    r.profiler.beginFrame()
    cmd := r.backend.Begin()

    st, views := r.extract(scene, cam)
    r.buildOverlay()

    target := r.acquireTarget()
    r.uploadSharedResources(cmd)
    r.encode(st, views, target, cmd)
    r.recordScreenshot(target, cmd)
    r.encodeOverlay(target, cmd)

    r.submit(cmd)
    r.finishFrame()
}
```

Someone reading it learns the whole process without reading any step, and reaches a step's details only by choosing to.

### Break functions into chunks

Separate the logical steps inside a function with blank lines, so its shape is visible before its details. A chunk that needs a heading comment to be understood is usually a function waiting to be extracted.

## Types and Structs

Group struct fields by **semantic usage**, not alphabetically or merely by type.

Related state belongs together:

```go
type Registry struct {
    users      map[string]*User
    usersCount int

    companies      map[string]*Company
    companiesCount int
}
```

rather than:

```go
type Registry struct {
    users     map[string]*User
    companies map[string]*Company

    usersCount     int
    companiesCount int
}
```

Prefer explicit field names in non-trivial composite literals:

```go
user := User{
    Name: name,
    Age:  age,
}
```

Choose pointer versus value receivers consistently for a type. Use pointer receivers when methods mutate the receiver, the value should not be copied, or the struct is sufficiently large. Small immutable value-like types may use value receivers. When uncertain, prefer a pointer receiver. ([Google GitHub Pages][2])

Use meaningful types to express intent. Prefer glm.Vec3f over [3]float32 when the type carries domain meaning.

### State is plain data; logic stays with its owner

A type that holds state holds only data. The algorithms that fill it, and the glue between one component's data and another's, belong to the component that makes the decisions.

State must not hold a view into another component's internals. A draw layout records the raster state its materials share — pool, cull, blend — not the renderer's pipeline index for it; the renderer turns one into the other when it draws.

### No parallel slices

Data that belongs together lives in one struct. Slices correlated by index make every reader keep them in step, and every writer liable to break them:

```go
// Bad: four slices that must agree index for index
materialIDs       []materials.ID
materialPipelines []uint32
materialPools     []*materials.Pool
materialBlends    []materials.BlendMode

// Bad: views and their buffers matched by position
views       []shadowView
viewBuffers []shadowViewBuffers

// Good: each view carries what belongs to it
type view struct {
    viewProj glm.Mat4f
    cull     *cullBuffers
}
```

The only exception is if writting parallel arrays yield substantial better performance, for instance, when aligning usage patters with CPU caches.

### Derive, don't duplicate

Do not store what can be computed from what is already stored. A copy has to be kept in sync, and eventually is not. If batches are ordered so that same-pipeline batches are adjacent, the spans of them are found by walking the batches, not kept in a second list; if each indirect command already holds its region's base, a separate table of region bases is the same numbers twice.

### One concept, one type

Two types holding the same data under different names are one type. When the only difference is which caller uses it, name it for what it holds: one `positionRoot` for every position-only pass, not a `shadowRoot` and an identical `debugIDRoot`.

### Track change where it happens

To know whether something derived is stale, let the thing that changes keep a revision counter, and compare counters. Keeping a copy of every input just to diff it each frame costs memory, time, and a second place to get wrong.

### Allocation and resources

On paths that run every frame, reuse scratch storage owned by a long-lived value rather than allocating; on rare paths, a local allocation is clearer. Ensure a resource where it is used. When an earlier step needs it first, ensure it there and say why.

## Interfaces

Keep interfaces small. Larger interfaces create weaker abstractions.

Define interfaces where they are **consumed**, unless the interface itself is an intentional public protocol.

Do not create interfaces preemptively merely to make concrete implementations appear abstract or mockable.

Prefer accepting the minimal interface required and returning concrete types:

```go
func Decode(r io.Reader) (*Document, error)
```

Avoid exporting interfaces that exist only for internal implementation details or testing. ([Google GitHub Pages][2])

Single-method interfaces should normally follow established Go naming conventions:

```go
Reader
Writer
Closer
Formatter
```

Do not reuse established method names such as `Read`, `Write`, or `String` with surprising semantics. ([Go][1])

## Errors

Use `error` for operations that can fail. It should conventionally be the final return value:

```go
func Load(path string) (*File, error)
```

Handle errors where meaningful; otherwise return them with useful context.

Error strings should normally begin lowercase and contain no trailing punctuation:

```go
return fmt.Errorf("load texture: %w", err)
```

Do not use `panic` for ordinary errors. Libraries should return errors to their callers rather than exposing internal panic/recover mechanisms. ([Google GitHub Pages][2])

## Context

When a function accepts `context.Context`, it is the first parameter and conventionally named `ctx`:

```go
func Load(ctx context.Context, path string) error
```

Do not store a context inside a struct. Pass it explicitly to operations that need it. ([Google GitHub Pages][2])

## Concurrency

Concurrency should simplify the design, not be introduced merely because Go makes goroutines inexpensive.

Prefer communication when ownership of data can naturally be transferred between goroutines. Use mutexes when protecting simple shared state is clearer.

Every goroutine should have a clear lifetime and termination condition.

Remember that concurrency and parallelism are different concerns. ([Go][1])

## Documentation

Document exported packages, types, functions, methods, constants, and variables when their purpose or contract is not already completely obvious.

Documentation should explain **behavior, contracts, constraints, and reasons**, rather than restating implementation.

Prefer comments that remain useful to callers:

```go
// Open loads the asset at path and returns an error if its format is unsupported.
func Open(path string) (*Asset, error)
```

Comments should not compensate for poor naming.

Documentation is written for users of the API. ([Go][1])

When an order, a placement or a constant is **extremely important**, the comment says so and gives the evidence — especially when it was measured rather than reasoned. Otherwise the next reader sees an arbitrary choice and undoes it:

```go
// The frame's closing timestamp goes in after the pass ends. Written as the last
// command inside it, KosmicKrisp dropped it intermittently (24 of 40 frames).
```

Delete code that has no callers. Version control remembers it; the reader should not have to.

## Tests

Tests **must use an external `_test` package**:

```go
package geometry_test
```

Never use:

```go
package geometry
```

Tests must exercise the package through its **public API**. Do not test unexported functions, fields, or implementation details directly.

This keeps tests aligned with observable behavior and prevents internals from becoming effectively frozen by the test suite.

Prefer straightforward tests over elaborate testing abstractions. Table-driven tests and subtests are useful when they improve readability, but should not be introduced mechanically.

Test failures should clearly identify the operation and expected result:

```go
if got != want {
    t.Errorf("Area() = %v, want %v", got, want)
}
```

Subtests must be independent and runnable individually. ([Google GitHub Pages][2])

A test must be able to fail. Before trusting a new test, run it against the code without the fix and watch it fail; a test that passes either way measures nothing.

Assert on the values themselves, not a lossy summary of them. A hash or a sum can match when the values do not — two different images can share a pixel sum.

## Guiding Principle

Optimize for the person reading the code.

Prefer:

**clear over clever, simple over abstract, conventional over novel, and precise over verbose.**

Names should carry exactly as much information as their context requires; APIs should explain themselves through their types and signatures; implementation details should remain implementation details. ([Gopher Guides][3])

[1]: https://go.dev/doc/effective_go "Effective Go - The Go Programming Language"
[2]: https://google.github.io/styleguide/go/decisions "Google Style Guides | Style guides for Google-originated open-source projects"
[3]: https://www.gopherguides.com/articles/assets/whats-in-a-name-dave-cheney/whats-in-a-name.pdf "What's in a name? Go Get Community"
