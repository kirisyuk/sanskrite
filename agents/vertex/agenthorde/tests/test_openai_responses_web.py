from __future__ import annotations

from openai.types.responses import Response
from openai.types.responses.response_output_message import ResponseOutputMessage
from openai.types.responses.response_output_text import AnnotationURLCitation, ResponseOutputText

from agenthorde_common.tools.openai_responses_web import citations_from_openai_response, output_text_from_openai_response


def test_citations_from_openai_response_collects_url_citations() -> None:
    ann = AnnotationURLCitation(
        type="url_citation",
        start_index=0,
        end_index=1,
        title="Example",
        url="https://example.com/path",
    )
    part = ResponseOutputText(type="output_text", text="See source.", annotations=[ann])
    msg = ResponseOutputMessage(
        id="m1",
        role="assistant",
        status="completed",
        type="message",
        content=[part],
    )
    r = Response.model_construct(id="r1", created_at=0.0, model="gpt-4o", object="response", output=[msg])
    cites = citations_from_openai_response(r)
    assert len(cites) == 1
    assert cites[0]["url"] == "https://example.com/path"
    assert cites[0]["title"] == "Example"
    assert cites[0]["domain"] == "example.com"
    assert output_text_from_openai_response(r) == "See source."
