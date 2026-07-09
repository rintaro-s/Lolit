using System;
using System.Collections.Generic;
using System.Windows.Forms;

namespace LolitSolidWorksAddin
{
    /// <summary>
    /// Shows the version history of the currently open file and lets the
    /// user download a past version -- either just that file, or (for
    /// assemblies) that file plus every part/sub-assembly it depended on
    /// at that exact version, laid out so SolidWorks can reopen it as-is.
    /// </summary>
    internal class HistoryDialog : Form
    {
        private readonly ListView _list = new ListView { View = View.Details, FullRowSelect = true, MultiSelect = false, Dock = DockStyle.Fill };
        private readonly Button _downloadOne = new Button { Text = "このバージョンをダウンロード", AutoSize = true };
        private readonly Button _downloadBundle = new Button { Text = "関連部品も含めてダウンロード", AutoSize = true };

        public event Action<VersionInfo, bool>? DownloadRequested; // (version, includeDependencies)

        public HistoryDialog(string fileName, List<VersionInfo> versions, bool isAssembly)
        {
            Text = $"{fileName} のバージョン履歴";
            Width = 600;
            Height = 440;
            StartPosition = FormStartPosition.CenterScreen;

            _list.Columns.Add("保存日時", 150);
            _list.Columns.Add("作成者", 110);
            _list.Columns.Add("メモ", 280);
            foreach (var v in versions)
            {
                var when = DateTimeOffset.FromUnixTimeSeconds(v.Ts).LocalDateTime.ToString("yyyy-MM-dd HH:mm");
                var item = new ListViewItem(when);
                item.SubItems.Add(v.Author);
                item.SubItems.Add(v.Message);
                item.Tag = v;
                _list.Items.Add(item);
            }
            if (_list.Items.Count > 0) _list.Items[0].Selected = true;

            var panel = new FlowLayoutPanel { Dock = DockStyle.Bottom, Height = 48, FlowDirection = FlowDirection.RightToLeft, Padding = new Padding(8) };
            _downloadBundle.Enabled = isAssembly;
            _downloadBundle.Click += (s, e) => Trigger(true);
            _downloadOne.Click += (s, e) => Trigger(false);
            panel.Controls.Add(_downloadOne);
            panel.Controls.Add(_downloadBundle);

            Controls.Add(_list);
            Controls.Add(panel);
        }

        private void Trigger(bool withDeps)
        {
            if (_list.SelectedItems.Count == 0)
            {
                MessageBox.Show(this, "バージョンを選択してください。", "Lolit");
                return;
            }
            var v = (VersionInfo)_list.SelectedItems[0].Tag!;
            DownloadRequested?.Invoke(v, withDeps);
        }
    }
}
