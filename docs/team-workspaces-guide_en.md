# Team Workspace Quick Guide

A Team uses one OpenClaw Lite Leader to coordinate multiple Workers around a shared goal. You can start from an immutable built-in template or generate a user-owned custom template from natural-language intent. The Leader understands the request, delegates work, collects member deliveries, handles recovery, and publishes the final result.

## Scope

- Collaboration is fixed to **Leader-mediated collaboration**: requests reach the Leader first, who coordinates the members.
- The Leader always uses **OpenClaw Lite**. Each Worker can use **OpenClaw Lite** or **Hermes Lite**.
- Hermes Lite is disabled with an explanation when no enabled Hermes Lite gateway image is configured.
- Built-in templates cannot be changed or deleted. Custom templates belong to the current user and can be refined, deleted, and reused.

## 1. Create a Team from a Built-in Template

1. Open **Teams** from the navigation and enter the creation page.
2. Enter a Team name and adjust shared storage when needed.
3. Choose a template and select an available Runtime for each Worker.
4. Review the summary and select **Create**.

The **+ Custom Team** action in the upper-right opens custom-template management. Worker Runtime selection is available in the member table on the same page.

![Built-in templates, the Custom Team entry, and Worker Runtime selection](./main/team-create-fixed-and-custom-entry.png)

Eight immutable built-in templates are available: Standard Two-Member, Delivery Three-Member, Product Discovery Four-Member, Quality Gate Four-Member, Full-stack Delivery Five-Member, API Integration Five-Member, Research Publication Six-Member, and Software Engineering Eight-Member. Templates already provide member responsibilities and role profiles, so individual resource-preset setup is not required.

## 2. Generate a Custom Team

Open **Custom Team** and describe what the Team should achieve. Leave the member count empty for automatic allocation, or select a total of 2–6 members.

![Generate a custom Team from natural language and a member count](./main/custom-team-generate.png)

Generation and responsibility adjustment use the current user's AI Gateway with `model: "auto"`. The gateway chooses the actual model, and that model's saved Thinking setting applies. The Custom Team page has no separate Thinking switch. If no model is available, the page asks the user to enable one in model management.

Every generated result keeps these rules:

- The Team contains 2–6 members.
- The first and only Leader keeps `memberId=leader`.
- Capability tags describe suitable abilities; they do not install Skills or modify Runtime configuration.

## 3. Manage Custom Templates

Select a template under **My Custom Teams** to:

- rename it;
- revise the whole Team after changing the intent or member count;
- regenerate the whole Team from the saved intent and count;
- delete it, or use it on the Team creation page.

![Manage existing custom Team templates](./main/custom-team-manage.png)

Each update creates a new template version. Built-in templates never appear in the editable list.

## 4. Refine Member Responsibilities

Expand a member and describe the requested change in natural language. Empty input produces an explicit prompt and is not submitted.

![Refine a member responsibility with natural language](./main/custom-team-member-adjustment.png)

The Leader can also be refined, but only its domain-specific extension changes. The Leader identity, immutable orchestration behavior, current Worker roster, delegation, collection, review, and final-synthesis relationships remain intact. After the Worker count changes, the existing Team bootstrap still gives the Leader the complete roster and responsibilities.

## 5. Start and Follow Collaboration

After creation, describe the goal to the Leader in Team chat. The Leader plans, delegates, collects deliveries and review evidence, and publishes the final synthesis. A Worker completion closes only that Worker's item; the root task completes after Leader synthesis.

The Team detail page includes:

- **Team chat** for plans, assignments, meaningful progress, deliveries, reviews, and final synthesis.
- **Execution Kanban**, whose header shows the current query and whose cards show root and member work state.
- **Query navigation** when two or more queries exist; a newly submitted query becomes the default selection.
- **Files** for shared artifacts. Markdown, text, and JSON can be previewed in the page; other files can be downloaded.

Monitor observes activity, completion receipts, and failure signals for reminders and recovery. It does not independently manufacture task success, failure, cancellation, or completion.

## 6. Hermes Lite Worker Sessions

Hermes Lite Team conversations use Hermes native session storage. Complete messages and tool results appear incrementally in the Hermes GUI while work is running, rather than only after the task becomes historical.

Opening the same Hermes Lite instance from a Team member or from the instance list shows and can continue the same Team session. Ordinary Hermes sessions retain their existing behavior. Sessions provide interaction and observability; Team control-plane state remains authoritative for Kanban and completion.

## 7. Recommendations

- Start with the closest built-in template; generate a custom template for specialized responsibilities you expect to reuse.
- State scope, data sources, output format, and acceptance criteria in the goal.
- Do not resend the same request merely because a Worker has delivered; allow the Leader to review and synthesize.
- Thinking can increase latency and reasoning-token usage. Configure it in model management according to the task.
