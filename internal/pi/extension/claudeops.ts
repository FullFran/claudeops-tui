/**
 * ClaudeOps extension for Pi / Gentle Shell.
 *
 * Displays active subscription quota and token usage inside Pi,
 * updates the status widget on turn completion, and registers the
 * `/claudeops` slash command to inspect 5h/7d windows and costs.
 */
import { execFile } from "node:child_process";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

function runClaudeops(args: string[]): Promise<string> {
	return new Promise((resolve, reject) => {
		const bin = process.env.CLAUDEOPS_BIN || "claudeops";
		execFile(bin, args, { timeout: 5000 }, (err, stdout) => {
			if (err) return reject(err);
			resolve(stdout.trim());
		});
	});
}

export default function (pi: ExtensionAPI) {
	const updateStatus = async (ctx: any) => {
		try {
			const line = await runClaudeops(["statusline", "--format", "compact"]);
			if (line && ctx?.ui?.setStatus) {
				ctx.ui.setStatus("claudeops", line);
			}
		} catch {
			// Status bar update is best-effort
		}
	};

	pi.on("session_start", async (_event, ctx) => {
		await updateStatus(ctx);
	});

	pi.on("turn_end", async (_event, ctx) => {
		await updateStatus(ctx);
	});

	pi.registerCommand("claudeops", {
		description: "Show ClaudeOps subscription quota and usage windows",
		handler: async (_args, ctx) => {
			try {
				const plain = await runClaudeops(["statusline", "--format", "plain"]);
				if (plain && ctx?.ui?.notify) {
					ctx.ui.notify(plain, "info");
				} else if (plain) {
					console.log(plain);
				} else if (ctx?.ui?.notify) {
					ctx.ui.notify("ClaudeOps: no active quota or session data available", "warning");
				}
			} catch (err: any) {
				if (ctx?.ui?.notify) {
					ctx.ui.notify(`ClaudeOps error: ${err.message || String(err)}`, "error");
				}
			}
		},
	});
}
