# Style Guidelines

This document outlines the coding and API design guidelines for Pix.

## Must Have

1. Do not expose struct fields publicly.
   All structs should keep fields unexported. Access to internal state must happen through getters/setters or methods. This ensures internal state tracking (e.g. dirty flags, cache invalidation, GPU synchronization) remains correct and consistent.

2. Do not expose WebGPU internals.
   Types from `wgpu.*` or other low-level GPU APIs must not be exposed to user code. Pix should provide a stable, high-level abstraction layer over WebGPU.

3. Do not export internal APIs.
   If a struct, function, interface, or type is not intended for end users, it should remain unexported.

4. Prefer simple and easy-to-read code
   Avoid using clever/complex code, unless is strictly necessary. In that case, a comment should explain why is that the case.

---

## Should Have

1. APIs should be flexible and runtime-friendly.
   Users should be able to modify objects dynamically during runtime whenever possible.

2. Use consistent naming for concepts.
   A concept should have a single canonical name throughout the codebase. Variables, methods, and types referring to the same concept should use the same terminology consistently.

3. Keep receiver names short and consistent.
   Receiver names should generally use one or two letters. Prefer a single letter when clear and readable.

   Examples:
   - `l` for all light types (`DirectionalLight`, `PointLight`, `AmbientLight`, etc.)
   - `m` for meshes
   - `c` for cameras

4. Flexibility must not significantly impact performance.
   Dynamic APIs should still allow high-performance usage patterns. For example, meshes may support an `isStatic` optimization flag to avoid unnecessary change detection every frame. Advanced users should be able to achieve highly optimized rendering paths.

5. Favor a CISC-style API design.
   Pix APIs should prioritize convenience and ergonomics, even if this results in additional helper methods.

   Examples:
   - `SetPositionX`
   - `SetPositionXY`
   - `SetPositionXYZ`

   A larger API surface is acceptable when it improves usability and developer experience.

---

## Nice to Have

1. Minimize the WebGPU surface area internally.
   Prefer high-level wrappers and centralized resource management. Object creation and disposal should happen in a limited number of well-defined places.

2. Keep core concepts in the main package. 
   Fundamental engine concepts should generally live in the main `pix` package for simplicity and discoverability.

   Non-essential or optional functionality should live in dedicated packages, for example:
   - camera controls
   - asset loaders
   - debugging tools
   - editor integrations

3. Prefer explicitness over magic.
   APIs should behave predictably and avoid hidden side effects. When behavior is non-obvious, it should be made explicit through naming or configuration.

4. Design for long-term API stability.
   Public APIs should be difficult to misuse and easy to evolve without breaking user code.