# Third-party notices

This feature incorporates the locked Desktop renderer source, shared modules and static resources from **Hermes Agent / Hermes Desktop**, by Nous Research and contributors, licensed under the MIT License.

- Repository: <https://github.com/NousResearch/hermes-agent>
- Source revision: `29112bef099274229cadff79cdff7bf7b99c4b77` (`v2026.8.31`)
- Upstream license: <https://github.com/NousResearch/hermes-agent/blob/29112bef099274229cadff79cdff7bf7b99c4b77/LICENSE>
- Exact imported files and source hashes: `upstream.lock.json`

The build verifies and copies the complete upstream license into the distributable as `HERMES-LICENSE.txt`. Source files are imported unchanged; reviewed browser capability gates are maintained separately in `scripts/web-adaptations.mjs`, with the browser bridge under `src/`. The real upstream App, entry point, layout and routes are retained. Hermes trademarks and application names remain owned by their respective owners. This adaptation is a ClawManager feature and is not presented as an official Nous Research Desktop distribution.

JavaScript dependencies and their immutable package integrity values are recorded in `package-lock.json`. Their individual license notices remain in their distributed package metadata. Production JavaScript builds preserve legal comments emitted by the bundler.
