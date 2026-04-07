// content-script is injected dynamically after 30 seconds of active reading
window.extractReadableContent = function() {
    try {
        // Readability expects a document clone so it doesn't mutate the live page layout
        const documentClone = document.cloneNode(true);
        const reader = new Readability(documentClone);
        const article = reader.parse();
        
        if (!article || !article.textContent) return null;
        
        // Chunking the raw text into manageable ~500 word bounds for AI tokens
        const words = article.textContent.split(/\s+/).filter(Boolean);
        const chunks = [];
        for (let i = 0; i < words.length; i += 500) {
            chunks.push(words.slice(i, i + 500).join(' '));
        }
        
        const scrollDepth = Math.round(
            ((window.scrollY + window.innerHeight) / document.documentElement.scrollHeight) * 100
        );
        
        return {
            extracted: true,
            method: "browser_extension_readability",
            text_chunks: chunks,
            word_count: words.length,
            scroll_depth_pct: scrollDepth || 0
        };
    } catch (e) {
        console.error('[UserMemory] Readability parsing failed:', e);
        return null;
    }
};
