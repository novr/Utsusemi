package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/novr/utsusemi/internal/config"
)

type configYAMLUpdate struct {
	key string
	del bool
	val any
}

func sharedFlagYAMLUpdates(cmd *cobra.Command, opts sharedConfigOptions) []configYAMLUpdate {
	var updates []configYAMLUpdate
	if cmd.Flags().Changed("labels") {
		updates = append(updates, configYAMLUpdate{key: "labels", val: splitLabels(opts.labels)})
	}
	if cmd.Flags().Changed("base-image") {
		updates = append(updates, configYAMLUpdate{key: "base_image", val: opts.baseImage})
	}
	if cmd.Flags().Changed("runner-version") {
		updates = append(updates, configYAMLUpdate{key: "runner_version", val: opts.runnerVer})
	}
	if cmd.Flags().Changed("pool-size") {
		updates = append(updates, configYAMLUpdate{key: "pool_size", val: opts.poolSize})
	}
	if cmd.Flags().Changed("mounts") {
		mounts := normalizeMounts(opts.mounts)
		if len(mounts) == 0 {
			updates = append(updates, configYAMLUpdate{key: "mounts", del: true})
		} else {
			updates = append(updates, configYAMLUpdate{key: "mounts", val: mounts})
		}
	}
	if cmd.Flags().Changed("softnet") {
		updates = append(updates, configYAMLUpdate{key: "softnet", val: opts.softnet})
	}
	if cmd.Flags().Changed("action-cache") {
		updates = append(updates, configYAMLUpdate{key: "action_cache", val: opts.actionCache})
	}
	if cmd.Flags().Changed("reclaim-policy") {
		updates = append(updates, configYAMLUpdate{key: "reclaim_policy", val: opts.reclaimPolicy})
	}
	if cmd.Flags().Changed("reclaim-grace") {
		updates = append(updates, configYAMLUpdate{key: "reclaim_grace", val: config.Duration(opts.reclaimGrace)})
	}
	if cmd.Flags().Changed("min-free-disk-gb") {
		updates = append(updates, configYAMLUpdate{key: "min_free_disk_gb", val: opts.minFreeDiskGB})
	}
	return updates
}

func applyConfigYAMLUpdates(raw []byte, updates []configYAMLUpdate) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	root, err := configYAMLMapping(&doc)
	if err != nil {
		return nil, err
	}
	text := splitYAML(raw)
	var splices []yamlSplice
	var extra []string
	for _, update := range updates {
		keyNode, valNode := lastMappingPair(root, update.key)
		if update.del {
			for _, pair := range mappingPairs(root, update.key) {
				start, end := keySpan(pair[0], pair[1])
				splices = append(splices, yamlSplice{start: start, end: end})
			}
			continue
		}
		rendered, err := renderYAMLValue(update.val)
		if err != nil {
			return nil, err
		}
		block := yamlValueIsBlock(update.val)
		if keyNode == nil {
			extra = append(extra, strings.Split(formatYAMLKey(update.key, rendered, "", block), "\n")...)
			continue
		}
		if !block && valNode != nil && valNode.Line == keyNode.Line && valNode.Column > 0 && !strings.Contains(rendered, "\n") {
			splices = append(splices, yamlSplice{
				start: keyNode.Line, end: keyNode.Line,
				scalar: true, column: valNode.Column,
				comment: valNode.LineComment, rendered: rendered,
			})
			continue
		}
		start := keyNode.Line
		end := start
		if valNode != nil {
			if last := nodeEndLine(valNode); last > end {
				end = last
			}
		}
		comment := keyNode.LineComment
		if comment == "" && valNode != nil {
			comment = valNode.LineComment
		}
		formatted := formatYAMLKey(update.key, rendered, comment, true)
		splices = append(splices, yamlSplice{start: start, end: end, lines: strings.Split(formatted, "\n")})
	}
	sortYAMLSplices(splices)
	if err := checkYAMLSplices(splices); err != nil {
		return nil, err
	}
	for _, splice := range splices {
		if err := text.apply(splice); err != nil {
			return nil, err
		}
	}
	if len(extra) > 0 {
		text.lines = append(text.lines, extra...)
		text.newline = true
	}
	out := text.bytes()
	if err := yaml.Unmarshal(out, &config.Config{}); err != nil {
		return nil, fmt.Errorf("patched config: %w", err)
	}
	return out, nil
}

type yamlSplice struct {
	start, end int
	lines      []string
	scalar     bool
	column     int
	comment    string
	rendered   string
}

func sortYAMLSplices(splices []yamlSplice) {
	for i := 1; i < len(splices); i++ {
		for j := i; j > 0 && splices[j].start > splices[j-1].start; j-- {
			splices[j], splices[j-1] = splices[j-1], splices[j]
		}
	}
}

func checkYAMLSplices(splices []yamlSplice) error {
	for i := 1; i < len(splices); i++ {
		if splices[i].end >= splices[i-1].start {
			return fmt.Errorf("config yaml updates overlap")
		}
	}
	return nil
}

func configYAMLMapping(doc *yaml.Node) (*yaml.Node, error) {
	node := doc
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) != 1 {
			return nil, fmt.Errorf("config yaml must contain one document")
		}
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config yaml must be a mapping")
	}
	return node, nil
}

func mappingPairs(mapping *yaml.Node, key string) [][2]*yaml.Node {
	var pairs [][2]*yaml.Node
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			pairs = append(pairs, [2]*yaml.Node{mapping.Content[i], mapping.Content[i+1]})
		}
	}
	return pairs
}

func lastMappingPair(mapping *yaml.Node, key string) (keyNode, valNode *yaml.Node) {
	pairs := mappingPairs(mapping, key)
	if len(pairs) == 0 {
		return nil, nil
	}
	return pairs[len(pairs)-1][0], pairs[len(pairs)-1][1]
}

func yamlValueIsBlock(v any) bool {
	_, ok := v.([]string)
	return ok
}

func keySpan(keyNode, valNode *yaml.Node) (start, end int) {
	start = keyNode.Line
	if keyNode.HeadComment != "" {
		start -= strings.Count(keyNode.HeadComment, "\n") + 1
	}
	if start < 1 {
		start = 1
	}
	end = keyNode.Line
	if valNode != nil {
		if last := nodeEndLine(valNode); last > end {
			end = last
		}
	}
	return start, end
}

func nodeEndLine(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	end := n.Line
	for _, child := range n.Content {
		if last := nodeEndLine(child); last > end {
			end = last
		}
	}
	return end
}

func renderYAMLValue(v any) (string, error) {
	raw, err := yaml.Marshal(v)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(raw), "\n"), nil
}

func formatYAMLKey(key, rendered, lineComment string, block bool) string {
	comment := ""
	if lineComment != "" {
		comment = " " + lineComment
	}
	if !block {
		return key + ": " + rendered + comment
	}
	var b strings.Builder
	b.WriteString(key)
	b.WriteByte(':')
	b.WriteString(comment)
	if rendered == "" {
		return b.String()
	}
	for _, line := range strings.Split(rendered, "\n") {
		b.WriteByte('\n')
		b.WriteString("  ")
		b.WriteString(line)
	}
	return b.String()
}

type yamlText struct {
	lines   []string
	newline bool
}

func splitYAML(raw []byte) yamlText {
	s := string(raw)
	newline := strings.HasSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return yamlText{newline: newline}
	}
	return yamlText{lines: strings.Split(s, "\n"), newline: newline}
}

func (t yamlText) bytes() []byte {
	if len(t.lines) == 0 {
		if t.newline {
			return []byte("\n")
		}
		return nil
	}
	s := strings.Join(t.lines, "\n")
	if t.newline {
		s += "\n"
	}
	return []byte(s)
}

func (t *yamlText) apply(splice yamlSplice) error {
	if splice.start < 1 || splice.end < splice.start || splice.end > len(t.lines) {
		return fmt.Errorf("config yaml line %d out of range", splice.start)
	}
	if splice.scalar {
		updated, err := spliceScalar(t.lines[splice.start-1], splice.column, splice.comment, splice.rendered)
		if err != nil {
			return err
		}
		t.lines[splice.start-1] = updated
		return nil
	}
	next := make([]string, 0, len(t.lines)-(splice.end-splice.start+1)+len(splice.lines))
	next = append(next, t.lines[:splice.start-1]...)
	next = append(next, splice.lines...)
	next = append(next, t.lines[splice.end:]...)
	t.lines = next
	return nil
}

func spliceScalar(line string, column int, lineComment, rendered string) (string, error) {
	if column < 1 || column > len(line)+1 {
		return "", fmt.Errorf("config yaml column %d out of range", column)
	}
	prefix := line[:column-1]
	tail := line[column-1:]
	suffix := ""
	if lineComment != "" {
		i := strings.LastIndex(tail, lineComment)
		if i < 0 {
			return "", fmt.Errorf("config yaml line comment %q not found", lineComment)
		}
		for i > 0 && (tail[i-1] == ' ' || tail[i-1] == '\t') {
			i--
		}
		suffix = tail[i:]
	}
	return prefix + rendered + suffix, nil
}
