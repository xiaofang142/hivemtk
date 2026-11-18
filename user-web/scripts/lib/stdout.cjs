const { Console } = require('node:console');

// 普通CLI输出明确走stdout，保留Node标准格式，不提升为警告或删除输出。
const output = new Console({ stdout: process.stdout, stderr: process.stderr });
exports.print = output.log.bind(output);
