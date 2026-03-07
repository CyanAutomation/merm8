#!/usr/bin/env node
process.stderr.write("FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory\\n");
process.stdout.write(JSON.stringify({valid:false,error:{message:"internal parser error: oom",line:0,column:0}}));
process.exit(1);
