#!/usr/bin/env node

import { isExpectedCliError, runCli } from "../src/cli.js";

try {
  process.exitCode = await runCli(process.argv.slice(2));
} catch (error) {
  process.stderr.write(`groundspec: ${error.message}\n`);
  process.exitCode = isExpectedCliError(error) ? 2 : 3;
}
