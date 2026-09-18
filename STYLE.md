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

## Guiding Principle

Optimize for the person reading the code.

Prefer:

**clear over clever, simple over abstract, conventional over novel, and precise over verbose.**

Names should carry exactly as much information as their context requires; APIs should explain themselves through their types and signatures; implementation details should remain implementation details. ([Gopher Guides][3])

[1]: https://go.dev/doc/effective_go "Effective Go - The Go Programming Language"
[2]: https://google.github.io/styleguide/go/decisions "Google Style Guides | Style guides for Google-originated open-source projects"
[3]: https://www.gopherguides.com/articles/assets/whats-in-a-name-dave-cheney/whats-in-a-name.pdf "What's in a name? Go Get Community"
