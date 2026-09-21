// One API Plus 的提交规范配置。
//
// 基于社区通用的 conventional commit，但针对本仓库做了一处调整：
// 关闭 subject-case。commitlint 的 sentence-case / start-case 判定对中文标题
// 完全没有意义（"One API Plus 第二阶段" 会被误判成 sentence-case），
// 而本项目的提交标题通常是中文短语，因此这条规则只制造噪音。
export default {
  extends: ["@commitlint/config-conventional"],
  rules: {
    "subject-case": [0, "never", []],
    // 中文标题常常一行放不下，放宽到 120
    "header-max-length": [2, "always", 120],
    // body 里的 Markdown 列表与表格不参与行数限制校验
    "body-max-line-length": [0, "always", Infinity],
    "footer-max-line-length": [0, "always", Infinity],
  },
};
