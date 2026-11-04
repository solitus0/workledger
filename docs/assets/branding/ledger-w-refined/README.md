# Ledger W refinement

Final icon assets for Workledger, 3 November 2026.

## Feedback applied

[ChatGPT’s critique](https://chatgpt.com/c/6aea1ee3-9df8-83eb-940b-d6868ecfc4eb) ranked Ledger W first, followed by Timefold and Prompt Ledger. It recommended an immediately legible W, compact proportions, fewer ledger divisions, disciplined geometry, flat violet, transparent backgrounds, and separate optical treatment at small sizes.

The revision has an approximately 1.3:1 ink silhouette, two aligned horizontal divisions, broad lower joins, and restrained corner rounding. The 16–24 px version removes the divisions. The generated direction was refined into native SVG geometry to provide uniform fills and genuine transparent cutouts.

The follow-up critique rated the design ready with minor polish and identified avatar framing as the necessary final change. A separate padded avatar now places the approved mark at approximately 76% of the square’s width, centered in lavender on a dark neutral background. Pixel checks at 256 and 1024 px found no mark pixels outside the centered circular crop. The primary mark geometry is identical to the reviewed version.

## Assets

| Use | SVG | PNG |
| --- | --- | --- |
| Light backgrounds, violet #6D4BC3 | [Primary](workledger-w-light.svg) | [1024 px](workledger-w-light.png) · [32 px](workledger-w-light-32.png) |
| Dark backgrounds, lavender #B9A2F4 | [Primary](workledger-w-dark.svg) | [1024 px](workledger-w-dark.png) · [32 px](workledger-w-dark-32.png) |
| Light backgrounds, small sizes | [Solid](workledger-w-small-light.svg) | [16 px](workledger-w-light-16.png) · [24 px](workledger-w-light-24.png) |
| Dark backgrounds, small sizes | [Solid](workledger-w-small-dark.svg) | [16 px](workledger-w-dark-16.png) · [24 px](workledger-w-dark-24.png) |
| Monochrome | [Near-black](workledger-w-black.svg) · [White](workledger-w-white.svg) | |
| Monochrome, small sizes | [Near-black](workledger-w-small-black.svg) · [White](workledger-w-small-white.svg) | |
| Avatar, lavender #B9A2F4 on dark #1E1E2E | [Padded avatar](workledger-w-avatar.svg) | [1024 px](workledger-w-avatar.png) · [256 px](workledger-w-avatar-256.png) |

[Review sheet](workledger-w-review.jpg) · [Editable review sheet](workledger-w-review.svg) · [Generated direction](generated-direction.png)

[Square and circular avatar proof](workledger-w-avatar-preview.png) · [Editable avatar proof](workledger-w-avatar-preview.svg)

## Usage

- Use the solid variant at 16–24 px.
- Use the divided variant at 32 px and above; the divisions are most comfortable at 64 px and above.
- Use violet on light backgrounds and lavender on dark backgrounds.
- Use the padded, fixed-background avatar for square avatars or circular crops.
- Use the transparent primary exports for the README and other uncropped placements.

The review sheet was inspected in a browser with 16, 24, 32, and 64 px samples on light and dark backgrounds. PNG export dimensions and alpha channels were checked. The small W remains identifiable; the segmented form is reserved for 32 px and above. The neutral Workledger wordmark shown on the sheet is a pairing study in system sans serif, not an outlined production wordmark.

The primary, small-size, and monochrome icon SVGs and their PNG exports have transparent backgrounds. Avatar exports use an opaque dark background. Review sheets and the generated direction image have opaque backgrounds. The original three exploration images remain in the parent directory.

## Generation prompt

Built-in ImageGen was used to edit the original Ledger W concept. The final SVG geometry and theme/size exports were then authored and rendered locally.

```text
Use case: logo-brand.
Asset type: refined primary Workledger brand icon, symbol only.
Input image: edit target and conceptual starting point, the previously explored Ledger W. This is a structural redesign of that mark based on a brand critique. Preserve its W plus horizontal ledger-entry rhythm and violet family, while substantially improving the proportions and legibility.

Primary request: create ONE compact, upright, unmistakable capital W monogram, roughly 1.3 times as wide as tall, settled and calm. Begin with a bold solid W whose two bottom valleys and central rising join are completely obvious. Bring the shoulders inward and remove the wing-like spread, sharp triangular fragments, and sweeping ribbon effect of the input. Use thick consistent diagonals, broad lower joins with subtle small consistent corner rounding. Add only TWO restrained horizontal ledger divisions, one in the upper section of each outer stroke, at exactly the same height. The divisions must have generous, consistent gaps and remain broad simple blocks, with no floating thin slivers. The W silhouette must dominate, with the ledger construction only a second read. Balanced, deliberate, memorable, precise, trustworthy developer-tool identity, not an energetic badge.

Style: exact flat vector-like silhouette, minimal geometric design, one perfectly uniform matte muted violet #6D4BC3 fill. Background: perfectly uniform white #FFFFFF. This is an opaque graphic. Center the mark on a square white canvas with 20% outer clear space. No letter caption, no wordmark, no other text.
Constraints: only this ONE W icon, not a design sheet. No wings, bird, esports emblem, shield, crypto badge, mountain, clock, book, terminal, sync arrows, checkmark, outer tile, texture, gradients, glow, shadows, bevels, lighting, grain, sketch marks or decorative elements. Avoid long pointed terminals and delicate cuts. Core acceptance: a viewer sees a W immediately at favicon scale.
```
